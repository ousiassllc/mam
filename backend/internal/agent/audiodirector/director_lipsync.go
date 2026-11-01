package audiodirector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/media"
)

// VideoClip is a clip of the Edit Timeline with the file of its Take.
type VideoClip struct {
	// Index is the 1-based position of the clip in the Edit Timeline.
	Index  int
	TakeID string
	// Path is the Take file (absolute).
	Path string
	// In and Out are the seconds of the Take the clip plays; Start is where it begins in the cut.
	In, Out, Start float64
}

// Length is how long the clip plays.
func (c VideoClip) Length() float64 { return c.Out - c.In }

// lipSync redoes the mouth of the clips where a character speaks (MAX QUALITY): a clip whose
// Dialogue lines all start and end inside it gets the lines placed at their moments as the audio
// Sync Labs follows. A clip with a line across its end is given up as a whole (a mouth redone
// without that line would not match the mix). The new picture replaces the Take's at export.
func (r *run) lipSync(ctx context.Context) error {
	if !r.scope.LipSync {
		r.skip(domain.LayerLipSync, "", domain.SkipPolicy, r.in.Quality.Label()+" では作りません")
		return nil
	}
	groups := map[int][]domain.AudioClip{} // clip index -> its lines
	crossed := map[int]bool{}              // clip index -> a line runs over its end
	for _, c := range r.tl.Clips {
		if c.LayerOf() != domain.LayerDialogue || c.Speaker == "" {
			continue
		}
		vi := slices.IndexFunc(r.in.Clips, func(v VideoClip) bool {
			return c.StartSeconds >= v.Start-lineTolerance && c.StartSeconds < v.Start+v.Length()
		})
		switch {
		case vi < 0:
			r.skip(domain.LayerLipSync, c.Text, domain.SkipError, "セリフがどのクリップにも入っていません")
		case c.End() > r.in.Clips[vi].Start+r.in.Clips[vi].Length()+lineTolerance:
			r.skip(domain.LayerLipSync, c.Text, domain.SkipError, "セリフがクリップの境目をまたぎます")
			crossed[r.in.Clips[vi].Index] = true
		default:
			idx := r.in.Clips[vi].Index
			groups[idx] = append(groups[idx], c)
		}
	}
	for idx := range crossed {
		delete(groups, idx)
	}
	if len(groups) == 0 {
		return nil
	}
	if r.d.Providers.LipSync == nil {
		r.skip(domain.LayerLipSync, "", domain.SkipError, "Lip Sync のプロバイダーが設定されていません")
		return nil
	}
	model := r.scope.LipSyncModel
	for _, vc := range r.in.Clips {
		lines := groups[vc.Index]
		if len(lines) == 0 {
			continue
		}
		cost := r.d.Prices.LipSync(model, vc.Length())
		release, ok, err := r.reserve(ctx, domain.LayerLipSync, lines[0].Text, cost)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		out, stop, err := r.syncClip(ctx, vc, lines, model, cost)
		release() // the cost is in the ledger now, or the call was not paid
		if err != nil {
			return err
		}
		if out != nil {
			r.tl.LipSync = append(r.tl.LipSync, *out)
		}
		if stop {
			break
		}
	}
	return nil
}

// syncClip lip-syncs one clip. The clip is nil when it was left out (listed in Skipped); stop is
// true when the following clips cannot be lip-synced either (no key). A call Sync Labs accepted is
// recorded at cost before its result is stored, so a local failure afterwards still counts.
func (r *run) syncClip(ctx context.Context, vc VideoClip, lines []domain.AudioClip, model string, cost float64) (out *domain.LipSyncClip, stop bool, err error) {
	dir := filepath.Join(r.d.Root, filepath.FromSlash(r.rel), fmt.Sprintf("lipsync-%02d-work", vc.Index))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, fmt.Errorf("create audio directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	label := lines[0].Text
	video := filepath.Join(dir, "clip.mp4")
	if _, err := r.d.Tools.Compose(ctx, []media.Segment{{Path: vc.Path, Start: vc.In, End: vc.Out}}, video); err != nil {
		return nil, false, r.localFailure(ctx, err, label)
	}
	voices := make([]media.Track, len(lines))
	for i, l := range lines {
		voices[i] = media.Track{Path: r.abs(l.Path), Start: l.StartSeconds - vc.Start}
	}
	audio := filepath.Join(dir, "voice.wav")
	if err := r.d.Tools.PlaceVoices(ctx, voices, vc.Length(), audio); err != nil {
		return nil, false, r.localFailure(ctx, err, label)
	}
	vb, err := os.ReadFile(video) // #nosec G304 -- a file this run just wrote
	if err != nil {
		return nil, false, fmt.Errorf("read clip: %w", err)
	}
	ab, err := os.ReadFile(audio) // #nosec G304 -- a file this run just wrote
	if err != nil {
		return nil, false, fmt.Errorf("read voice: %w", err)
	}
	data, err := r.d.Providers.LipSync.Sync(ctx, LipSyncRequest{Model: model, Video: Sample{Filename: "clip.mp4", Data: vb}, Audio: Sample{Filename: "voice.wav", Data: ab}})
	var charged *ChargedError
	if err == nil || errors.As(err, &charged) {
		// Sync Labs has accepted (and bills) the clip, whether or not a video came back: the ledger
		// gets it even when the download failed, the job timed out or the run was cancelled meanwhile.
		if err := r.spend.record(ctx, domain.ModelSyncLabs, cost); err != nil {
			return nil, false, err
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	if errors.Is(err, ErrNoSyncLabsKey) {
		r.skip(domain.LayerLipSync, "", domain.SkipError, "Sync Labs の API キーが設定されていません（設定画面で synclabs のキーを登録してください）")
		return nil, true, nil
	}
	if err != nil {
		r.skip(domain.LayerLipSync, label, domain.SkipError, err.Error())
		return nil, false, nil
	}
	rel := path.Join(r.rel, fmt.Sprintf("lipsync-%02d.mp4", vc.Index))
	if err := fsutil.WriteFileAtomic(r.abs(rel), data, 0o600); err != nil {
		return nil, false, fmt.Errorf("write lip sync: %w", err)
	}
	info, err := r.d.Tools.Probe(ctx, r.abs(rel))
	if err != nil {
		_ = os.Remove(r.abs(rel))
		r.skip(domain.LayerLipSync, label, domain.SkipError, "Lip Sync の映像を読み取れませんでした: "+err.Error())
		return nil, false, nil
	}
	return &domain.LipSyncClip{ClipIndex: vc.Index, TakeID: vc.TakeID, Path: rel, DurationSeconds: info.Duration, Model: domain.ModelSyncLabs}, false, nil
}

// localFailure lists a clip that ffmpeg could not prepare; a cancelled run is an error.
func (r *run) localFailure(ctx context.Context, err error, label string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	r.skip(domain.LayerLipSync, label, domain.SkipError, "Lip Sync の入力を作れませんでした: "+err.Error())
	return nil
}
