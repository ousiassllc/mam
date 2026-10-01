package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
)

// rough returns the rendered Rough Cut of the Edit Timeline (a draft with the sound of the Takes),
// made on first use. It is also what the preview plays.
func (s *EditorService) rough(ctx context.Context, ver domain.EditTimelineVersion) (string, media.Info, error) {
	if s.tools == nil {
		return "", media.Info{}, ErrUnavailable
	}
	dst := s.previewPath("edit-" + ver.ID + ".mp4")
	if dst == "" {
		return "", media.Info{}, ErrNotFound
	}
	unlock := lockFor(&s.preview, dst)
	defer unlock()
	if info, err := s.tools.Probe(ctx, dst); err == nil {
		return dst, info, nil
	}
	cuts, err := s.cuts(ctx, ver.Timeline, nil)
	if err != nil {
		return "", media.Info{}, err
	}
	info, err := s.tools.Render(ctx, cuts, media.RenderOptions{Draft: true}, dst)
	if err != nil {
		return "", media.Info{}, &AIError{Reason: "Rough Cut の書き出しに失敗しました: " + err.Error(), Err: err}
	}
	return dst, info, nil
}

// transitionNames maps the Edit Timeline transitions to ffmpeg's.
var transitionNames = map[domain.TransitionType]string{
	domain.TransitionCut: "cut", domain.TransitionFade: "fade", domain.TransitionFadeBlack: "fadeblack", domain.TransitionDissolve: "dissolve",
}

// cuts turns the Edit Timeline into what Render takes. A clip whose Take file is gone fails the
// stage. A clip that Lip Sync redid (lipSync) plays that picture instead of its Take's.
func (s *EditorService) cuts(ctx context.Context, tl domain.EditTimeline, lipSync []domain.LipSyncClip) ([]media.Cut, error) {
	sources, err := s.loadSources(ctx, clipTakeIDs(tl.Clips))
	if err != nil {
		return nil, err
	}
	cuts := make([]media.Cut, len(tl.Clips))
	for i, c := range tl.Clips {
		src, ok := sources.take(ctx, c.TakeID)
		if !ok {
			return nil, conflict("クリップ %d の Take のファイルが見つかりません。編集し直してください", i+1)
		}
		cut := media.Cut{Path: s.assetPath(src.path), Start: c.In, End: c.Out, Transition: transitionNames[c.Transition.Type], Overlap: c.Transition.Seconds, Caption: c.Caption}
		if j := slices.IndexFunc(lipSync, func(l domain.LipSyncClip) bool { return l.ClipIndex == i+1 && l.TakeID == c.TakeID }); j >= 0 {
			if !s.fileExists(lipSync[j].Path) {
				return nil, conflict("クリップ %d の Lip Sync の映像ファイルが見つかりません。音声を作り直してください", i+1)
			}
			cut.Path, cut.Start, cut.End = s.assetPath(lipSync[j].Path), 0, min(c.Out-c.In, lipSync[j].DurationSeconds)
		}
		cuts[i] = cut
	}
	return cuts, nil
}

// Preview returns the rendered Rough Cut of the Edit Timeline version (GET /edit-timelines/:id/preview).
func (s *EditorService) Preview(ctx context.Context, id string) (MediaFile, error) {
	ver, err := s.editors.GetEditTimeline(ctx, id)
	if err != nil {
		return MediaFile{}, notFound(err)
	}
	file, _, err := s.rough(ctx, ver)
	if err != nil {
		return MediaFile{}, err
	}
	return MediaFile{Path: file, MimeType: "video/mp4"}, nil
}

// ExportInput is what the user chooses for an export; an empty field takes the default (the
// Brief's aspect ratio, 1080p, H.264).
type ExportInput struct {
	// BranchID is the Branch whose Edit and Audio Timelines are exported ("" for the trunk).
	BranchID    string
	AspectRatio string
	Resolution  string
	Codec       string
}

// Export writes the finished video to the assets directory (POST /projects/:id/export) with the
// chosen aspect ratio, resolution and codec and moves the Project to EXPORTED. The Project must be
// READY (or EXPORTED, to export again). The file appears only once it is complete.
func (s *EditorService) Export(ctx context.Context, projectID string, in ExportInput) (EditorView, error) {
	if s.tools == nil {
		return EditorView{}, ErrUnavailable
	}
	release, err := s.acquire(projectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if project.State != domain.StateReady && project.State != domain.StateExported {
		return EditorView{}, conflict("書き出せるのは Final QC に合格した後（READY）です（現在: %s）", project.State)
	}
	opts, err := s.exportOptions(ctx, projectID, in)
	if err != nil {
		return EditorView{}, err
	}
	rec, err := s.renderExport(ctx, projectID, in.BranchID, opts)
	if err != nil {
		return EditorView{}, err
	}
	if _, err := s.editors.CreateExport(context.WithoutCancel(ctx), rec); err != nil {
		_ = os.Remove(s.assetPath(rec.FilePath))
		return EditorView{}, notFound(err)
	}
	if project.State == domain.StateReady {
		if err := s.projects.Transition(ctx, projectID, domain.StateExported); err != nil {
			return EditorView{}, err
		}
	}
	s.announce(ctx, projectID, fmt.Sprintf("書き出しが完了しました（%s / %s / %s）。ダウンロード: /projects/%s/exports/%s/video", opts.AspectRatio, opts.Resolution, opts.Codec, projectID, rec.ID))
	return s.View(ctx, projectID, "")
}

// exportOptions are the Brief's export options with the user's choices applied and checked.
func (s *EditorService) exportOptions(ctx context.Context, projectID string, in ExportInput) (domain.ExportOptions, error) {
	_, opts, err := s.briefOptions(ctx, projectID)
	if err != nil {
		return opts, err
	}
	if in.AspectRatio != "" {
		opts.AspectRatio = in.AspectRatio
	}
	if in.Resolution != "" {
		opts.Resolution = in.Resolution
	}
	if in.Codec != "" {
		opts.Codec = in.Codec
	}
	if !slices.Contains(domain.ExportAspectRatios(), opts.AspectRatio) {
		return opts, invalid("aspect_ratio", "アスペクト比は %s から選んでください", strings.Join(domain.ExportAspectRatios(), " / "))
	}
	if !slices.Contains(domain.ExportResolutions(), opts.Resolution) {
		return opts, invalid("resolution", "解像度は %s から選んでください", strings.Join(domain.ExportResolutions(), " / "))
	}
	if !slices.Contains(domain.ExportCodecs(), opts.Codec) {
		return opts, invalid("codec", "コーデックは %s から選んでください", strings.Join(domain.ExportCodecs(), " / "))
	}
	return opts, nil
}

// renderExport renders the latest Edit and Audio Timelines of the series to the export file and
// returns the record to store for it.
func (s *EditorService) renderExport(ctx context.Context, projectID, branchID string, opts domain.ExportOptions) (domain.Export, error) {
	w, h, err := opts.Size()
	if err != nil {
		return domain.Export{}, invalid("", "%s", err.Error())
	}
	edit, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if err != nil {
		return domain.Export{}, notFound(err)
	}
	audio, err := s.editors.LatestAudioTimeline(ctx, projectID, branchID)
	if err != nil {
		return domain.Export{}, notFound(err)
	}
	cuts, err := s.cuts(ctx, edit.Timeline, audio.Timeline.LipSync)
	if err != nil {
		return domain.Export{}, err
	}
	ro := media.RenderOptions{Width: w, Height: h, Codec: opts.Codec}
	if audio.Timeline.MixPath != "" {
		if !s.fileExists(audio.Timeline.MixPath) {
			return domain.Export{}, conflict("ミックス済みの音声ファイルが見つかりません。音声を作り直して Final QC をやり直してください")
		}
		ro.Audio = s.assetPath(audio.Timeline.MixPath)
	}
	id := domain.NewID()
	rel := path.Join(projectID, "exports", id+"."+domain.ExportExtension(opts.Codec))
	if _, err := s.tools.Render(ctx, cuts, ro, s.assetPath(rel)); err != nil {
		return domain.Export{}, &AIError{Reason: "動画の書き出しに失敗しました: " + err.Error(), Err: err}
	}
	return domain.Export{ID: id, ProjectID: projectID, BranchID: branchID, FilePath: rel, Resolution: opts.Resolution, AspectRatio: opts.AspectRatio, Codec: opts.Codec}, nil
}

// ExportFile returns the exported video (GET /projects/:id/exports/:exportId/video).
func (s *EditorService) ExportFile(ctx context.Context, projectID, exportID string) (MediaFile, error) {
	e, err := s.editors.GetExport(ctx, projectID, exportID)
	if err != nil {
		return MediaFile{}, notFound(err)
	}
	if !s.fileExists(e.FilePath) {
		return MediaFile{}, ErrNotFound
	}
	mime := "video/mp4"
	if domain.ExportExtension(e.Codec) == "webm" {
		mime = "video/webm"
	}
	return MediaFile{Path: s.assetPath(e.FilePath), MimeType: mime}, nil
}
