package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// previewDir is where the previews of Takes (proxies and thumbnails) are cached, under the assets
// directory. They are made on demand and can be deleted at any time.
const previewDir = ".previews"

// TakeService is the Take System (F-15, spec/spec.md §14): the comparison of the Takes of a Shot,
// the user's choice among them, and Partial Take Use.
type TakeService struct {
	*core
	takes       *repository.TakeRepository
	shots       *repository.GenerationRepository
	generations *GenerationService
	chatSvc     *ChatService
	// tools is ffmpeg / ffprobe; nil when they are not installed. Partial Take Use then fails with
	// ErrUnavailable, and a 4K Take is played as it is (no proxy, no thumbnail).
	tools *media.Tools
	// preview serializes the making of one preview file: two requests for the same Take wait for
	// one ffmpeg run.
	preview sync.Map // path -> *sync.Mutex
	// compose serializes Partial Take Use per Shot.
	compose sync.Map // shot ID -> *sync.Mutex
	// afterSelect runs after a Take was selected: the Editor → Reshoot Loop may have been waiting for it.
	afterSelect func()
}

// TakeView is a Take of the comparison.
type TakeView struct {
	domain.TakeInfo
	// Label names the Take in the comparison and in words: "A", "B", ...
	Label string
	// FPS is the frame rate of the file (0 when it could not be read).
	FPS float64
}

// Comparison is the Takes of a Shot side by side.
type Comparison struct {
	Project domain.Project
	Shot    domain.Shot
	Takes   []TakeView
	// PartialUse tells whether Partial Take Use is offered: ffmpeg is installed and a Take can be cut.
	PartialUse bool
}

// Compare returns the Takes of the Shot, oldest first. The size, length and frame rate of a file
// the Model Adapter did not describe are read from the file.
func (s *TakeService) Compare(ctx context.Context, shotID string) (Comparison, error) {
	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return Comparison{}, notFound(err)
	}
	takes, err := s.takes.ShotTakes(ctx, shotID)
	if err != nil {
		return Comparison{}, err
	}
	project, err := s.projects.Get(ctx, ref.ProjectID)
	if err != nil {
		return Comparison{}, notFound(err)
	}
	out := Comparison{Project: project, Shot: ref.Shot, Takes: make([]TakeView, len(takes))}
	for i, t := range takes {
		v := TakeView{TakeInfo: t, Label: domain.TakeLabel(i)}
		if abs := s.assetPath(t.FilePath); s.tools != nil && abs != "" {
			if info, err := s.tools.Probe(ctx, abs); err == nil {
				v.FPS = info.FPS
				if v.Resolution == "" && info.Height > 0 {
					v.Resolution = fmt.Sprintf("%dp", info.Height)
				}
				if v.DurationSeconds == 0 {
					v.DurationSeconds = info.Duration
				}
			}
		}
		out.Takes[i] = v
		out.PartialUse = out.PartialUse || (s.tools != nil && t.Usable())
	}
	return out, nil
}

// Select makes the Take the one the Shot uses (POST /takes/:id/select). It is the user's choice
// and replaces the AI Pick or an earlier choice. A Take that is rejected, whose file was archived,
// or that already is the selected one is refused, and no intervention is recorded for it.
func (s *TakeService) Select(ctx context.Context, takeID string) (ShotFragment, error) {
	override := s.overridesAIPick(ctx, takeID)
	shotID, err := s.takes.Select(ctx, takeID)
	switch {
	case errors.Is(err, repository.ErrConflict):
		return ShotFragment{}, invalid("", "この Take は選択できません（除外済み、ファイルのアーカイブ済み、またはすでに選択済みです）")
	case err != nil:
		return ShotFragment{}, notFound(err)
	}
	s.recordIntervention(ctx, domain.Intervention{ShotID: shotID, TakeID: takeID, Kind: domain.InterventionSelectTake, Override: override})
	s.takeSelected()
	return s.generations.Shot(ctx, shotID)
}

// takeSelected wakes the editing follow-ups (the Editor → Reshoot Loop, the AUTO chain) that may
// have been waiting for a Selected Take. Every path that makes a Take the Shot's Selected Take
// (Select, UsePartial) calls it; a Candidate Take (a Partial Regeneration splice) selects nothing.
func (s *TakeService) takeSelected() {
	if s.afterSelect != nil {
		s.afterSelect()
	}
}

// overridesAIPick tells whether selecting the Take contradicts the AI Pick of its Shot. A Take it
// cannot read is no override: the selection itself reports the error.
func (s *TakeService) overridesAIPick(ctx context.Context, takeID string) bool {
	d, err := s.takes.Get(ctx, takeID)
	if err != nil {
		return false
	}
	takes, err := s.takes.ShotTakes(ctx, d.ShotID)
	return err == nil && domain.OverridesAIPick(takes, takeID)
}

// Reject excludes the Take (DELETE /takes/:id). Its file and history stay. Rejecting the AI Pick
// contradicts the AI. A Take that is already rejected, or whose file was archived, is refused, and
// no intervention is recorded for it.
func (s *TakeService) Reject(ctx context.Context, takeID string) (ShotFragment, error) {
	override := false
	if d, err := s.takes.Get(ctx, takeID); err == nil {
		override = d.Status == domain.TakeAIPick
	}
	shotID, err := s.takes.Reject(ctx, takeID)
	switch {
	case errors.Is(err, repository.ErrConflict):
		return ShotFragment{}, invalid("", "この Take は除外できません（すでに除外済み、またはファイルがアーカイブ済みです）")
	case err != nil:
		return ShotFragment{}, notFound(err)
	}
	s.recordIntervention(ctx, domain.Intervention{ShotID: shotID, TakeID: takeID, Kind: domain.InterventionRejectTake, Override: override})
	return s.generations.Shot(ctx, shotID)
}

// PartialInput is a Partial Take Use: the parts to join, given as segments or as words
// ("Take A の 0-2秒 + Take B の 2-4秒"); Segments win when both are given.
type PartialInput struct {
	Segments    []domain.TakeSegment
	Instruction string
}

// UsePartial joins parts of several Takes of the Shot into one new Take and makes it the Shot's
// Take (POST /shots/:id/takes/partial). The joints are cross-faded so that the cut is not seen
// (UC-3). The new Take is announced in the chat.
func (s *TakeService) UsePartial(ctx context.Context, shotID string, in PartialInput) (ShotFragment, error) {
	if s.tools == nil {
		return ShotFragment{}, ErrUnavailable
	}
	unlock := lockFor(&s.compose, shotID)
	defer unlock()

	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return ShotFragment{}, notFound(err)
	}
	takes, err := s.takes.ShotTakes(ctx, shotID)
	if err != nil {
		return ShotFragment{}, err
	}
	segs := in.Segments
	if len(segs) == 0 {
		if strings.TrimSpace(in.Instruction) == "" {
			return ShotFragment{}, invalid("instruction", "区間を指定してください")
		}
		if segs, err = domain.ParseTakeSegments(in.Instruction, takes); err != nil {
			return ShotFragment{}, invalid("instruction", "%s", err.Error())
		}
	}
	if err := domain.ValidateTakeSegments(segs, takes); err != nil {
		return ShotFragment{}, invalid("segments", "%s", err.Error())
	}

	if _, err := s.composeTake(ctx, ref, takes, segs, composeOptions{
		Title: fmt.Sprintf("Shot %d · 部分採用 Take", ref.Shot.OrderIndex+1),
	}); err != nil {
		return ShotFragment{}, err
	}
	s.recordIntervention(ctx, domain.Intervention{ShotID: shotID, Kind: domain.InterventionPartialTake})
	s.takeSelected() // the joined Take is the Shot's Selected Take now, as after Select
	return s.generations.Shot(ctx, shotID)
}

// composeOptions is how composeTake records the Take it makes.
type composeOptions struct {
	Title string
	// Candidate adds the Take without selecting it (a Partial Regeneration splice).
	Candidate bool
	// Source and RegeneratedTakeID say in the model parameters how the Take came to be.
	Source, RegeneratedTakeID string
}

// composeTake cuts the segments out of their Takes, joins them into a new video file and stores it
// as a new Take of the Shot, announced in the chat. The Takes it is made from are not touched. The
// caller holds the Shot's compose lock and has validated the segments.
func (s *TakeService) composeTake(ctx context.Context, ref repository.ShotRef, takes []domain.TakeInfo, segs []domain.TakeSegment, o composeOptions) (string, error) {
	assetID := domain.NewID()
	rel := filepath.ToSlash(filepath.Join(ref.ProjectID, assetID+".mp4"))
	dst := s.assetPath(rel)
	parts := make([]media.Segment, len(segs))
	byID := make(map[string]domain.TakeInfo, len(takes))
	for _, t := range takes {
		byID[t.ID] = t
	}
	for i, seg := range segs {
		path := s.assetPath(byID[seg.TakeID].FilePath)
		if path == "" {
			return "", invalid("", "Take のファイルがないため、合成できません")
		}
		parts[i] = media.Segment{Path: path, Start: seg.Start, End: seg.End}
	}
	info, err := s.tools.Compose(ctx, parts, dst)
	if err != nil {
		return "", &AIError{Reason: "Take の合成に失敗しました: " + err.Error()}
	}
	params, err := json.Marshal(struct {
		Kind              string               `json:"kind"`
		Source            string               `json:"source,omitempty"`
		RegeneratedTakeID string               `json:"regenerated_take_id,omitempty"`
		Resolution        string               `json:"resolution"`
		DurationSeconds   float64              `json:"duration_seconds"`
		Segments          []domain.TakeSegment `json:"segments"`
	}{domain.PartialTakeKind, o.Source, o.RegeneratedTakeID, fmt.Sprintf("%dp", info.Height), info.Duration, segs})
	if err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	var size int64
	if st, err := os.Stat(dst); err == nil {
		size = st.Size()
	}
	takeID, err := s.takes.CreateComposite(context.WithoutCancel(ctx), repository.NewComposite{
		ProjectID: ref.ProjectID, ShotID: ref.Shot.ID, BaseTakeID: segs[0].TakeID, Parameters: params, Candidate: o.Candidate,
		Asset: domain.Asset{ID: assetID, ProjectID: ref.ProjectID, Type: domain.AssetVideo, FilePath: rel, MimeType: "video/mp4", SizeBytes: size},
	})
	if err != nil {
		_ = os.Remove(dst)
		return "", notFound(err)
	}
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindTake, RefID: takeID,
		Content: domain.EncodeTakeCardBody(domain.TakeCardBody{
			Title:     o.Title,
			Summary:   describeSegments(segs, takes),
			ShotID:    ref.Shot.ID,
			VideoURL:  domain.TakeVideoURL(takeID),
			PosterURL: domain.TakeThumbnailURL(takeID),
		}),
	}
	if err := s.chatSvc.Announce(context.WithoutCancel(ctx), ref.ProjectID, card); err != nil {
		s.logger.Error("post partial take card failed", "take_id", takeID, "err", err)
	}
	return takeID, nil
}

// SpliceRegenerated is the last step of a Partial Regeneration (repair.Splicer). The Take
// regeneratedTakeID is a new Take of the whole Shot made to fix the ranges of baseTakeID; this adds
// one more Take that shows baseTakeID everywhere but in the ranges, where it shows the new one (the
// joints are cross-faded, like Partial Take Use). Neither of the two Takes is changed, and the
// added Take is a candidate: it does not take the place of the Shot's selected Take. It returns ""
// when no splice is worth making (see domain.SpliceSegments), and ErrUnavailable without ffmpeg.
func (s *TakeService) SpliceRegenerated(ctx context.Context, shotID, baseTakeID, regeneratedTakeID string, ranges []domain.TimeRange) (string, error) {
	if s.tools == nil {
		return "", ErrUnavailable
	}
	unlock := lockFor(&s.compose, shotID)
	defer unlock()

	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return "", notFound(err)
	}
	takes, err := s.takes.ShotTakes(ctx, shotID)
	if err != nil {
		return "", err
	}
	byID := make(map[string]domain.TakeInfo, len(takes))
	for _, t := range takes {
		byID[t.ID] = t
	}
	base, regenerated := byID[baseTakeID], byID[regeneratedTakeID]
	if base.FilePath == "" || regenerated.FilePath == "" {
		return "", invalid("", "Take のファイルがないため、部分再生成をつなぎ込めません")
	}
	length := func(t domain.TakeInfo) float64 {
		path := s.assetPath(t.FilePath)
		if path == "" {
			return 0
		}
		info, err := s.tools.Probe(ctx, path)
		if err != nil {
			return 0
		}
		return info.Duration
	}
	segs := domain.SpliceSegments(baseTakeID, length(base), regeneratedTakeID, length(regenerated), ranges)
	if segs == nil {
		return "", nil
	}
	return s.composeTake(ctx, ref, takes, segs, composeOptions{
		Title:     fmt.Sprintf("Shot %d · 部分再生成 Take", ref.Shot.OrderIndex+1),
		Candidate: true, Source: "partial_regeneration", RegeneratedTakeID: regeneratedTakeID,
	})
}

// describeSegments is "Take A 0.0-2.0秒 + Take B 2.0-4.0秒".
func describeSegments(segs []domain.TakeSegment, takes []domain.TakeInfo) string {
	label := map[string]string{}
	for i, t := range takes {
		label[t.ID] = domain.TakeLabel(i)
	}
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = fmt.Sprintf("Take %s %.1f-%.1f秒", label[s.TakeID], s.Start, s.End)
	}
	return strings.Join(parts, " + ")
}

// MediaFile is a file to serve.
type MediaFile struct {
	Path     string
	MimeType string
}

// Video returns the file the preview plays for the Take: the file itself, or, when the picture is
// taller than 1080 lines (4K), a 1080p proxy made on first use so that playback does not drop
// frames (spec/requirements/non-functional.md §2). Without ffmpeg the file itself is played.
func (s *TakeService) Video(ctx context.Context, takeID string) (MediaFile, error) {
	d, err := s.takes.Get(ctx, takeID)
	if err != nil {
		return MediaFile{}, notFound(err)
	}
	if d.FilePath == "" {
		return MediaFile{}, ErrNotFound // archived
	}
	src := s.assetPath(d.FilePath)
	if src == "" {
		return MediaFile{}, ErrNotFound // outside the assets directory
	}
	mime := "video/mp4"
	if a, err := s.assets.Get(ctx, d.ProjectID, d.AssetID); err == nil && a.MimeType != "" {
		mime = a.MimeType
	}
	if s.tools == nil {
		return MediaFile{Path: src, MimeType: mime}, nil
	}
	proxied := d.Proxied()
	if d.Resolution == "" { // not reported by the model: look at the file
		if info, err := s.tools.Probe(ctx, src); err == nil {
			proxied = info.Height > media.MaxPreviewHeight
		}
	}
	if !proxied {
		return MediaFile{Path: src, MimeType: mime}, nil
	}
	dst := s.previewPath(takeID + ".mp4")
	if dst == "" {
		return MediaFile{Path: src, MimeType: mime}, nil // the ID is not a safe file name: no proxy is made
	}
	if err := s.makePreview(dst, func() error { return s.tools.Proxy(ctx, src, dst) }); err != nil {
		s.logger.Error("make proxy failed; playing the original", "take_id", takeID, "err", err)
		return MediaFile{Path: src, MimeType: mime}, nil
	}
	return MediaFile{Path: dst, MimeType: "video/mp4"}, nil
}

// Thumbnail returns a JPEG of the first frame of the Take (ErrUnavailable without ffmpeg).
func (s *TakeService) Thumbnail(ctx context.Context, takeID string) (MediaFile, error) {
	d, err := s.takes.Get(ctx, takeID)
	if err != nil {
		return MediaFile{}, notFound(err)
	}
	if d.FilePath == "" {
		return MediaFile{}, ErrNotFound
	}
	if s.tools == nil {
		return MediaFile{}, ErrUnavailable
	}
	src := s.assetPath(d.FilePath)
	if src == "" {
		return MediaFile{}, ErrNotFound // outside the assets directory
	}
	dst := s.previewPath(takeID + ".jpg")
	if dst == "" {
		return MediaFile{}, ErrNotFound // the ID is not a safe file name
	}
	if err := s.makePreview(dst, func() error { return s.tools.Thumbnail(ctx, src, dst, 0) }); err != nil {
		return MediaFile{}, fmt.Errorf("make thumbnail: %w", err)
	}
	return MediaFile{Path: dst, MimeType: "image/jpeg"}, nil
}

// makePreview runs make unless the preview file already exists; concurrent calls for one file wait
// for the first.
func (s *TakeService) makePreview(dst string, make func() error) error {
	unlock := lockFor(&s.preview, dst)
	defer unlock()
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	return make()
}

// lockFor locks the mutex of key in m (created on first use) and returns its unlock.
func lockFor(m *sync.Map, key string) func() {
	mu, _ := m.LoadOrStore(key, &sync.Mutex{})
	l := mu.(*sync.Mutex)
	l.Lock()
	return l.Unlock
}
