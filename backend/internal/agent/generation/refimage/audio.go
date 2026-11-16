package refimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/audiolength"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Audio is the sound a Shot with audio is made from (Issue #1153), read for one run like the images are: the bytes as a model input, with the kind read from the bytes
// (never from the row), and its length, which is the length of the clip of a model whose clip is as long as its sound.
type Audio struct {
	AssetID string
	Input   modeladapter.Input
	// Seconds is the length of the sound rounded up to 0.1 s (audiolength.Of).
	Seconds float64
}

// audioSource is where the sound files are and how long one may be.
type audioSource struct {
	root  string
	limit func() int
}

// WithAudio lets r read the audio of Shots: the files are under root (the assets directory) and may be limit() seconds long at most (gentest.max_audio_seconds). It returns r.
func (r *Reader) WithAudio(root string, limit func() int) *Reader {
	r.audio = audioSource{root: root, limit: limit}
	return r
}

// Audio reads the sound the Shot (its shot_spec) is tied to: nil with no error when it has none. A tie that cannot be used is an error that names it (apperr input_missing:
// the asset or its file is gone, or it is no audio; input_unsupported: the length cannot be read or is over the limit), never a missing sound: a generation that dropped the
// sound would make a clip that ignores it, and one that failed in the adapter would do so after the job was queued. A database error is returned and a later call asks again.
func (r *Reader) Audio(ctx context.Context, shotSpec json.RawMessage) (*Audio, error) {
	id := shotaudio.ID(shotSpec)
	if id == "" {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.sounds[id]; ok {
		return a, nil
	}
	asset, err := r.assets.Get(ctx, r.projectID, id)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, missingAudio(id, "アセットが見つかりません（削除されたか、別のプロジェクトのものです）")
	case err != nil:
		return nil, err
	case asset.Type != domain.AssetAudio:
		return nil, missingAudio(id, "音声ファイルではありません")
	}
	data, err := r.readAudio(asset)
	if err != nil {
		r.logger.Warn("audio of a Shot unavailable", "asset_id", id, "err", err)
		return nil, missingAudio(id, "ファイルを読めません")
	}
	limit := audioLimit(r.audio)
	seconds, err := audiolength.Of(data, limit)
	if err != nil {
		return nil, err
	}
	mime, _ := audiofile.Sniff(data) // readable: Of has read its length
	a := &Audio{AssetID: id, Input: modeladapter.Input{Data: data, MimeType: mime}, Seconds: seconds}
	if r.sounds == nil {
		r.sounds = map[string]*Audio{}
	}
	r.sounds[id] = a
	return a, nil
}

func audioLimit(s audioSource) int {
	if s.limit == nil {
		return modeladapter.DefaultConfig().Gentest.AudioSeconds()
	}
	return s.limit()
}

// missingAudio is the refusal of a Shot whose audio cannot be used: it says which asset and why, and that the audio has to be chosen again (or taken off).
func missingAudio(assetID, why string) error {
	return apperr.New(apperr.CodeInputMissing).With("param", "audio").
		WithCause(fmt.Sprintf("この Shot に結び付けた音声ファイル（アセット %s）を使えません: %s。Shot の音声を選び直すか、外してから生成してください。", assetID, why)).
		WithDetail("the audio asset of the Shot is unusable: " + assetID + ": " + why)
}

// readAudio reads the file of the asset: under the assets directory, at most audiofile.MaxBytes.
func (r *Reader) readAudio(a domain.Asset) ([]byte, error) {
	if r.audio.root == "" || !filepath.IsLocal(filepath.FromSlash(a.FilePath)) {
		return nil, fmt.Errorf("asset %s has no readable path", a.ID)
	}
	abs := filepath.Join(r.audio.root, filepath.FromSlash(a.FilePath))
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > audiofile.MaxBytes {
		return nil, fmt.Errorf("asset %s is not a regular file of at most %d bytes", a.ID, audiofile.MaxBytes)
	}
	return os.ReadFile(abs) // #nosec G304 -- the path is the asset row's, under the assets directory
}

// ClipSeconds is the length of the clip model m makes for a Shot shotSeconds long with this audio (nil: none): the length of the sound for a model whose clip is as long as its
// sound, else the Shot's length snapped to the model's lengths (Config.ClipSeconds). The estimates of the strategy use it so that they match what Prepare requests.
func (a *Audio) ClipSeconds(cfg modeladapter.Config, m domain.ModelName, shotSeconds float64, framed bool) float64 {
	if caps, ok := cfg.VideoCaps(m); a != nil && ok && caps.AudioLength {
		return a.Seconds
	}
	return cfg.ClipSeconds(m, shotSeconds, framed)
}

// ApplyAudio puts the sound of the Shot (Audio) in the job; a Shot without audio leaves it as it is.
func (r *Reader) ApplyAudio(ctx context.Context, shotSpec json.RawMessage, job *modeladapter.VideoJob) error {
	a, err := r.Audio(ctx, shotSpec)
	if a != nil {
		job.Audio, job.AudioSeconds = &a.Input, a.Seconds
	}
	return err
}

// Voice is Audio and, for a Shot with audio, whether any of the References can be read as an image: the one picture a model of a sound draws its speaker from (a Keyframe, else a
// reference of the Pack). picture is false without a sound. A database error is returned.
func (r *Reader) Voice(ctx context.Context, shotSpec json.RawMessage, refs []domain.Reference) (sound *Audio, picture bool, err error) {
	if sound, err = r.Audio(ctx, shotSpec); sound == nil || err != nil {
		return nil, false, err
	}
	for _, ref := range refs {
		if _, ok, err := r.Input(ctx, ref); ok || err != nil {
			return sound, ok, err
		}
	}
	return sound, false, nil
}
