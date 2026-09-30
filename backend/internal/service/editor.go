package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/editor"
	"github.com/ousiassllc/mam/backend/internal/agent/finalqc"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrStageBusy means another editing stage (Rough Cut, judge, audio, Final QC, export) of the
// Project is running. HTTP 409.
var ErrStageBusy = errors.New("service: an editing stage is running")

// ConflictError is ErrConflict with the reason to show: the request is valid, but the Project is
// not in a state it can be done in.
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return "service: conflict: " + e.Message }

// Is makes errors.Is(err, ErrConflict) hold.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

func conflict(format string, args ...any) error {
	return &ConflictError{Message: fmt.Sprintf(format, args...)}
}

// EditorService runs the editing half of the pipeline: Rough Cut, editing, Editor Judge, simple
// Audio, Final QC and Export (F-18 Simple, F-20 Simple, F-26; spec/requirements/functional.md §3.6).
// One stage of a Project runs at a time.
type EditorService struct {
	*core
	editors *repository.EditorRepository
	shots   *repository.GenerationRepository
	takes   *repository.TakeRepository
	judge   *editor.Judge
	// audio is nil without ffmpeg.
	audio   *audiodirector.Director
	chatSvc *ChatService
	// tools is ffmpeg / ffprobe; nil when they are not installed.
	tools   *media.Tools
	stage   sync.Map // project ID -> *sync.Mutex
	preview sync.Map // preview file -> *sync.Mutex
}

// acquire claims the Project's editing stage; ErrStageBusy when another one runs.
func (s *EditorService) acquire(projectID string) (release func(), err error) {
	mu, _ := s.stage.LoadOrStore(projectID, &sync.Mutex{})
	l := mu.(*sync.Mutex)
	if !l.TryLock() {
		return nil, ErrStageBusy
	}
	return l.Unlock, nil
}

func (s *EditorService) assetPath(rel string) string {
	return filepath.Join(s.assetsDir, filepath.FromSlash(rel))
}

func (s *EditorService) fileExists(rel string) bool {
	if rel == "" {
		return false
	}
	st, err := os.Stat(s.assetPath(rel))
	return err == nil && st.Mode().IsRegular()
}

// ClipView is a clip of the Edit Timeline with what the editor shows of its Shot.
type ClipView struct {
	domain.EditClip
	Label     string
	Objective string
	// Start is the second the clip begins at in the finished video.
	Start        float64
	VideoURL     string
	ThumbnailURL string
}

// TimelineView is an Edit Timeline version for the editor.
type TimelineView struct {
	domain.EditTimelineVersion
	Clips    []ClipView
	Duration float64
	// Judge is the latest Editor Judge result of this version (nil: not judged yet).
	Judge *domain.EditorJudgeResult
	// Editable is false once the Project has moved past editing (a card waits, or it is READY).
	Editable bool
}

// editable reports whether a Project in the state can take an edit (see toEditing).
func editable(state domain.ProjectState) bool {
	switch state {
	case domain.StateEditing, domain.StateVisualDesign, domain.StateGenerating, domain.StateJudging,
		domain.StateEditJudging, domain.StateAudio, domain.StateFinalQC:
		return true
	}
	return false
}

// EditorView is the editing state of a Project: what the panel shows.
type EditorView struct {
	Project  domain.Project
	Timeline *TimelineView
	Audio    *domain.AudioTimelineVersion
	QC       *domain.FinalQCResult
	Exports  []domain.Export
	// Defaults are the export options preselected: the Brief's aspect ratio, 1080p, H.264.
	Defaults domain.ExportOptions
	// CanRender is false without ffmpeg: no preview, audio or export.
	CanRender bool
}

func (s *EditorService) briefOptions(ctx context.Context, projectID string) (domain.Brief, domain.ExportOptions, error) {
	brief, err := s.projects.GetBrief(ctx, projectID)
	if err != nil {
		return domain.Brief{}, domain.ExportOptions{}, notFound(err)
	}
	opts := domain.ExportOptions{AspectRatio: "16:9", Resolution: "1080p", Codec: domain.CodecH264}
	if ar := strings.ReplaceAll(strings.TrimSpace(brief.AspectRatio), "：", ":"); slices.Contains(domain.ExportAspectRatios(), ar) {
		opts.AspectRatio = ar
	}
	return brief, opts, nil
}

// View returns the editing state of the Project (GET /projects/:id/timeline).
func (s *EditorService) View(ctx context.Context, projectID string) (EditorView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	_, defaults, err := s.briefOptions(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	v := EditorView{Project: project, Defaults: defaults, CanRender: s.tools != nil}
	if tl, err := s.Timeline(ctx, projectID); err == nil {
		v.Timeline = &tl
	} else if !errors.Is(err, ErrNotFound) {
		return EditorView{}, err
	}
	if a, err := s.editors.LatestAudioTimeline(ctx, projectID); err == nil {
		v.Audio = &a
	} else if !errors.Is(err, repository.ErrNotFound) {
		return EditorView{}, err
	}
	if qc, err := s.editors.LatestFinalQC(ctx, projectID); err == nil {
		v.QC = &qc
	} else if !errors.Is(err, repository.ErrNotFound) {
		return EditorView{}, err
	}
	if v.Exports, err = s.editors.ListExports(ctx, projectID); err != nil {
		return EditorView{}, err
	}
	return v, nil
}

// shotLabels names every Shot of the Project ("Scene 1 · Shot 2") and remembers what the clips need.
type shotIndex struct {
	byID   map[string]repository.ShotRef
	labels map[string]string
	order  []repository.ShotRef
}

func (s *EditorService) shotIndex(ctx context.Context, projectID string) (shotIndex, error) {
	refs, err := s.shots.ListProjectShots(ctx, projectID)
	if err != nil {
		return shotIndex{}, err
	}
	idx := shotIndex{byID: make(map[string]repository.ShotRef, len(refs)), labels: make(map[string]string, len(refs)), order: refs}
	for _, r := range refs {
		idx.byID[r.Shot.ID] = r
		idx.labels[r.Shot.ID] = fmt.Sprintf("Scene %d · Shot %d", r.Scene.OrderIndex+1, r.Shot.OrderIndex+1)
	}
	return idx, nil
}

// Timeline returns the latest Edit Timeline of the Project for the editor (ErrNotFound before the
// first Rough Cut).
func (s *EditorService) Timeline(ctx context.Context, projectID string) (TimelineView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return TimelineView{}, err
	}
	ver, err := s.editors.LatestEditTimeline(ctx, projectID)
	if err != nil {
		return TimelineView{}, notFound(err)
	}
	return s.timelineView(ctx, ver)
}

func (s *EditorService) timelineView(ctx context.Context, ver domain.EditTimelineVersion) (TimelineView, error) {
	idx, err := s.shotIndex(ctx, ver.ProjectID)
	if err != nil {
		return TimelineView{}, err
	}
	project, err := s.getProject(ctx, ver.ProjectID)
	if err != nil {
		return TimelineView{}, err
	}
	v := TimelineView{EditTimelineVersion: ver, Editable: editable(project.State), Clips: make([]ClipView, len(ver.Timeline.Clips)), Duration: ver.Timeline.Duration()}
	starts := ver.Timeline.Starts()
	for i, c := range ver.Timeline.Clips {
		cv := ClipView{EditClip: c, Start: starts[i], Label: idx.labels[c.ShotID], VideoURL: domain.TakeVideoURL(c.TakeID), ThumbnailURL: domain.TakeThumbnailURL(c.TakeID)}
		if cv.Label == "" {
			cv.Label = fmt.Sprintf("クリップ %d", i+1)
		}
		if r, ok := idx.byID[c.ShotID]; ok {
			cv.Objective = r.Shot.Objective
		}
		v.Clips[i] = cv
	}
	if j, err := s.editors.LatestEditorJudge(ctx, ver.ID); err == nil {
		v.Judge = &j
	} else if !errors.Is(err, repository.ErrNotFound) {
		return TimelineView{}, err
	}
	return v, nil
}

// toEditing moves the Project to EDITING through the states in between: no stage of the
// pipeline sets JUDGING for the Project as a whole, so a Project whose Shots are done is
// carried along. Other states cannot be edited from.
func (s *EditorService) toEditing(ctx context.Context, p domain.Project) error {
	var path []domain.ProjectState
	switch p.State {
	case domain.StateEditing:
		return nil
	case domain.StateVisualDesign:
		path = []domain.ProjectState{domain.StateGenerating, domain.StateJudging, domain.StateEditing}
	case domain.StateGenerating:
		path = []domain.ProjectState{domain.StateJudging, domain.StateEditing}
	case domain.StateJudging, domain.StateEditJudging, domain.StateAudio, domain.StateFinalQC:
		path = []domain.ProjectState{domain.StateEditing}
	case domain.StateAwaitingApproval:
		return conflict("承認待ちのカードがあります。チャットで応答してから編集してください")
	default:
		return conflict("この状態（%s）では編集できません", p.State)
	}
	for _, next := range path {
		if err := s.projects.Transition(ctx, p.ID, next); err != nil {
			return err
		}
	}
	return nil
}

// cutSource is what a clip needs of its Take.
type cutSource struct {
	takeID  string
	path    string // relative to the assets directory
	seconds float64
}

// selectedSource reads the Selected Take of the Shot and its length. The second result is false
// when the Shot has none or its file is gone.
func (s *EditorService) selectedSource(ctx context.Context, sh domain.Shot) (cutSource, bool, error) {
	if sh.SelectedTakeID == "" {
		return cutSource{}, false, nil
	}
	return s.takeSource(ctx, sh.SelectedTakeID)
}

func (s *EditorService) takeSource(ctx context.Context, takeID string) (cutSource, bool, error) {
	d, err := s.takes.Get(ctx, takeID)
	if errors.Is(err, repository.ErrNotFound) {
		return cutSource{}, false, nil
	}
	if err != nil {
		return cutSource{}, false, err
	}
	src := cutSource{takeID: takeID, path: d.FilePath, seconds: d.DurationSeconds}
	if !s.fileExists(d.FilePath) {
		return src, false, nil
	}
	if s.tools != nil {
		info, err := s.tools.Probe(ctx, s.assetPath(d.FilePath))
		if err != nil {
			return src, false, nil // unreadable: as good as gone
		}
		src.seconds = info.Duration
	}
	return src, true, nil
}

// RoughCut makes the Rough Cut from the Selected Takes of every Shot (POST /projects/:id/edit/rough-cut)
// and starts EDITING. Every Shot needs a Selected Take with a file.
func (s *EditorService) RoughCut(ctx context.Context, projectID string) (EditorView, error) {
	release, err := s.acquire(projectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	idx, err := s.shotIndex(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if len(idx.order) == 0 {
		return EditorView{}, invalid("", "Shot がまだありません。Scene / Shot を作成してから Rough Cut を作ってください")
	}
	cuts := make([]editor.ShotCut, 0, len(idx.order))
	var missing []string
	for _, r := range idx.order {
		src, ok, err := s.selectedSource(ctx, r.Shot)
		if err != nil {
			return EditorView{}, err
		}
		if !ok {
			missing = append(missing, idx.labels[r.Shot.ID])
			continue
		}
		cuts = append(cuts, editor.ShotCut{ShotID: r.Shot.ID, TakeID: src.takeID, SceneIndex: r.Scene.OrderIndex, Planned: r.Shot.Duration, TakeSeconds: src.seconds})
	}
	if len(missing) > 0 {
		return EditorView{}, invalid("", "Selected Take（ファイルのある採用 Take）がない Shot があります: %s", strings.Join(missing, "、"))
	}
	tl, err := editor.RoughCut(cuts).Normalize()
	if err != nil {
		return EditorView{}, invalid("", "%s", timelineMessage(err))
	}
	if err := s.toEditing(ctx, project); err != nil {
		return EditorView{}, err
	}
	ver, err := s.editors.SaveEditTimeline(ctx, projectID, "", tl)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	s.announce(ctx, projectID, fmt.Sprintf("Rough Cut を作成しました（%d クリップ / %.1f 秒）。タイムラインで並べ替え・Cut Point・Transition を編集できます。", len(tl.Clips), tl.Duration()))
	return s.viewAfter(ctx, projectID, ver.ID)
}

// timelineMessage is the message of an invalid timeline without its sentinel prefix.
func timelineMessage(err error) string {
	return strings.TrimPrefix(err.Error(), domain.ErrInvalidTimeline.Error()+": ")
}

func (s *EditorService) viewAfter(ctx context.Context, projectID, _ string) (EditorView, error) {
	return s.View(ctx, projectID)
}

// announce posts a message of the editing stage in the chat. A failure is logged, not returned:
// the stage has been done.
func (s *EditorService) announce(ctx context.Context, projectID, text string) {
	if err := s.chatSvc.Announce(context.WithoutCancel(ctx), projectID, assistantText(text)); err != nil {
		s.logger.Error("post editor notice failed", "project_id", projectID, "err", err)
	}
}

// SaveTimeline saves an edited timeline as the next version (PUT /edit-timelines/:id). id is the
// version the editor was showing: when a newer one exists the save is refused (ErrConflict), so
// that an old editor cannot overwrite a newer edit. Saving takes the Project back to EDITING.
func (s *EditorService) SaveTimeline(ctx context.Context, id string, tl domain.EditTimeline) (TimelineView, error) {
	base, err := s.editors.GetEditTimeline(ctx, id)
	if err != nil {
		return TimelineView{}, notFound(err)
	}
	release, err := s.acquire(base.ProjectID)
	if err != nil {
		return TimelineView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, base.ProjectID)
	if err != nil {
		return TimelineView{}, err
	}
	idx, err := s.shotIndex(ctx, project.ID)
	if err != nil {
		return TimelineView{}, err
	}
	for i := range tl.Clips {
		c := &tl.Clips[i]
		r, ok := idx.byID[c.ShotID]
		if !ok {
			return TimelineView{}, invalid("clips", "クリップ %d の Shot がこの Project にありません", i+1)
		}
		src, ok, err := s.takeSource(ctx, c.TakeID)
		if err != nil {
			return TimelineView{}, err
		}
		if !ok {
			return TimelineView{}, invalid("clips", "クリップ %d（%s）の Take のファイルが見つかりません", i+1, idx.labels[r.Shot.ID])
		}
		if own, err := s.takes.ShotTakes(ctx, r.Shot.ID); err != nil {
			return TimelineView{}, err
		} else if !slices.ContainsFunc(own, func(t domain.TakeInfo) bool { return t.ID == c.TakeID }) {
			return TimelineView{}, invalid("clips", "クリップ %d の Take は %s の Take ではありません", i+1, idx.labels[r.Shot.ID])
		}
		c.SourceSeconds = src.seconds // the file decides; the client's value is not trusted
	}
	norm, err := tl.Normalize()
	if err != nil {
		return TimelineView{}, invalid("clips", "%s", timelineMessage(err))
	}
	if latest, err := s.editors.LatestEditTimeline(ctx, project.ID); err != nil {
		return TimelineView{}, notFound(err)
	} else if latest.ID != id {
		return TimelineView{}, conflict("別の編集が保存されています。画面を更新してから編集し直してください")
	}
	if err := s.toEditing(ctx, project); err != nil {
		return TimelineView{}, err
	}
	ver, err := s.editors.SaveEditTimeline(ctx, project.ID, id, norm)
	switch {
	case errors.Is(err, repository.ErrStale):
		return TimelineView{}, conflict("別の編集が保存されています。画面を更新してから編集し直してください")
	case err != nil:
		return TimelineView{}, notFound(err)
	}
	s.recordIntervention(ctx, domain.Intervention{ProjectID: project.ID, Kind: domain.InterventionEditTimeline, Override: true})
	return s.timelineView(ctx, ver)
}

// shotFacts is what the Editor Judge needs to know about the Shots of the clips.
func (s *EditorService) shotFacts(ctx context.Context, idx shotIndex, tl domain.EditTimeline) (map[string]editor.ShotFacts, error) {
	facts := make(map[string]editor.ShotFacts, len(tl.Clips))
	for _, c := range tl.Clips {
		r, ok := idx.byID[c.ShotID]
		if !ok {
			continue
		}
		f := editor.ShotFacts{Label: idx.labels[c.ShotID], Objective: r.Shot.Objective, SelectedTakeID: r.Shot.SelectedTakeID}
		_, f.TakeReady, _ = s.takeSource(ctx, c.TakeID)
		facts[c.ShotID] = f
	}
	return facts, nil
}

// JudgeTimeline runs the Editor Judge on the latest Edit Timeline (POST /edit-timelines/:id/judge).
// PASS moves the Project on to AUDIO (through the Rough Cut checkpoint when the Confirmation
// Level asks for it); Re-edit keeps it in EDITING with the findings in the chat.
func (s *EditorService) JudgeTimeline(ctx context.Context, id string) (EditorView, error) {
	ver, err := s.editors.GetEditTimeline(ctx, id)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	projectID := ver.ProjectID
	release, err := s.acquire(projectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if latest, err := s.editors.LatestEditTimeline(ctx, projectID); err != nil {
		return EditorView{}, notFound(err)
	} else if latest.ID != id {
		return EditorView{}, conflict("この編集より新しい版があります。画面を更新してから判定してください")
	}
	if project.State != domain.StateEditing && project.State != domain.StateEditJudging {
		return EditorView{}, conflict("編集中（EDITING）の Project だけ判定できます（現在: %s）", project.State)
	}
	brief, _, err := s.briefOptions(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	idx, err := s.shotIndex(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	facts, err := s.shotFacts(ctx, idx, ver.Timeline)
	if err != nil {
		return EditorView{}, err
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateEditJudging); err != nil {
		return EditorView{}, err
	}
	verdict, findings, err := s.judge.Judge(ctx, editor.JudgeInput{Brief: brief, Template: project.ProductionTemplate, Timeline: ver.Timeline, Shots: facts})
	if err != nil {
		s.backTo(ctx, projectID, domain.StateEditing)
		return EditorView{}, aiError(err)
	}
	res, err := s.editors.SaveEditorJudge(ctx, id, verdict, findings)
	if err != nil {
		s.backTo(ctx, projectID, domain.StateEditing)
		return EditorView{}, err
	}
	if res.Verdict != domain.EditorPass {
		if err := s.projects.Transition(ctx, projectID, domain.StateEditing); err != nil {
			return EditorView{}, err
		}
		s.announce(ctx, projectID, "Editor Judge: Re-edit（編集で直せる問題があります）\n"+describeFindings(res.Findings))
		return s.View(ctx, projectID)
	}
	summary := fmt.Sprintf("Rough Cut（%d クリップ / %.1f 秒）が Editor Judge に合格しました。音声制作に進んでよいですか？", len(ver.Timeline.Clips), ver.Timeline.Duration())
	posted, err := s.chatSvc.RequestApproval(ctx, projectID, domain.CheckpointRoughCut, summary, id)
	if err != nil {
		s.backTo(ctx, projectID, domain.StateEditing)
		return EditorView{}, err
	}
	if !posted {
		if err := s.projects.Transition(ctx, projectID, domain.StateAudio); err != nil {
			return EditorView{}, err
		}
		s.announce(ctx, projectID, "Editor Judge: PASS。音声制作に進みます。")
	}
	return s.View(ctx, projectID)
}

// backTo puts the Project back into a state after a stage failed. It works on a context that
// survives the request, and only logs: the failure being reported is the caller's.
func (s *EditorService) backTo(ctx context.Context, projectID string, state domain.ProjectState) {
	if err := s.projects.Transition(context.WithoutCancel(ctx), projectID, state); err != nil {
		s.logger.Error("restore project state failed", "project_id", projectID, "state", state, "err", err)
	}
}

func describeFindings(findings []domain.EditorFinding) string {
	lines := make([]string, len(findings))
	for i, f := range findings {
		where := "全体"
		if f.ClipIndex > 0 {
			where = fmt.Sprintf("クリップ %d", f.ClipIndex)
		}
		lines[i] = fmt.Sprintf("・%s: %s → %s", where, f.Issue, f.Action)
	}
	return strings.Join(lines, "\n")
}

// rough returns the rendered Rough Cut of the Edit Timeline (a draft with the sound of the Takes),
// made on first use. It is also what the preview plays.
func (s *EditorService) rough(ctx context.Context, ver domain.EditTimelineVersion) (string, media.Info, error) {
	if s.tools == nil {
		return "", media.Info{}, ErrUnavailable
	}
	dst := filepath.Join(s.assetsDir, previewDir, "edit-"+ver.ID+".mp4")
	unlock := lockFor(&s.preview, dst)
	defer unlock()
	if info, err := s.tools.Probe(ctx, dst); err == nil {
		return dst, info, nil
	}
	cuts, err := s.cuts(ctx, ver.Timeline)
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

// cuts turns the Edit Timeline into what Render takes. A clip whose Take file is gone fails the stage.
func (s *EditorService) cuts(ctx context.Context, tl domain.EditTimeline) ([]media.Cut, error) {
	cuts := make([]media.Cut, len(tl.Clips))
	for i, c := range tl.Clips {
		src, ok, err := s.takeSource(ctx, c.TakeID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, conflict("クリップ %d の Take のファイルが見つかりません。編集し直してください", i+1)
		}
		cuts[i] = media.Cut{Path: s.assetPath(src.path), Start: c.In, End: c.Out, Transition: transitionNames[c.Transition.Type], Overlap: c.Transition.Seconds}
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

// GenerateAudio makes the simple Audio Timeline for the latest Edit Timeline (POST
// /projects/:id/audio/generate): the Dialogue and Narration of the script spoken by ElevenLabs,
// mixed with the sound of the Rough Cut and normalized to the target loudness. The Project must
// be in AUDIO (the Editor Judge passed).
func (s *EditorService) GenerateAudio(ctx context.Context, projectID string) (EditorView, error) {
	if s.audio == nil || s.tools == nil {
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
	if project.State != domain.StateAudio {
		return EditorView{}, conflict("音声を作れるのは Editor Judge に合格した後（AUDIO）です（現在: %s）", project.State)
	}
	ver, err := s.editors.LatestEditTimeline(ctx, projectID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	roughPath, info, err := s.rough(ctx, ver)
	if err != nil {
		return EditorView{}, err
	}
	idx, err := s.shotIndex(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	cues, err := s.cues(ctx, projectID, idx, ver.Timeline)
	if err != nil {
		return EditorView{}, err
	}
	tl, err := s.audio.Produce(ctx, audiodirector.Input{
		ProjectID: projectID, EditTimelineID: ver.ID, Cues: cues,
		Rough: roughPath, HasSound: info.HasAudio, Seconds: info.Duration,
	})
	if err != nil {
		return EditorView{}, audioError(err)
	}
	if _, err := s.editors.SaveAudioTimeline(ctx, projectID, tl); err != nil {
		return EditorView{}, notFound(err)
	}
	var text string
	if n := len(tl.Clips); n > 0 {
		text = fmt.Sprintf("音声を作成しました（Dialogue / Narration %d 行、ラウドネス %.1f LUFS）。", n, tl.MeasuredLUFS)
	} else if tl.MixPath != "" {
		text = fmt.Sprintf("音声を作成しました（セリフなし。Take の音声のラウドネスを %.1f LUFS に整えました）。", tl.MeasuredLUFS)
	} else {
		text = "音声を作成しました（セリフも Take の音声もないため、無音の動画になります）。"
	}
	s.announce(ctx, projectID, text)
	return s.View(ctx, projectID)
}

// cues are the script lines of each Scene with the second the Scene begins at in the Rough Cut. A
// Scene none of whose Shots is in the timeline is not spoken.
func (s *EditorService) cues(ctx context.Context, projectID string, idx shotIndex, tl domain.EditTimeline) ([]audiodirector.Cue, error) {
	script, err := s.plans.LatestScript(ctx, projectID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	starts := tl.Starts()
	sceneStart := map[int]float64{}
	for i, c := range tl.Clips {
		r, ok := idx.byID[c.ShotID]
		if !ok {
			continue
		}
		if _, seen := sceneStart[r.Scene.OrderIndex]; !seen {
			sceneStart[r.Scene.OrderIndex] = starts[i]
		}
	}
	var cues []audiodirector.Cue
	for i, sc := range script.Doc.Script.Scenes {
		start, ok := sceneStart[i]
		if !ok || len(sc.Dialogue) == 0 {
			continue
		}
		cues = append(cues, audiodirector.Cue{Start: start, Lines: sc.Dialogue})
	}
	slices.SortFunc(cues, func(a, b audiodirector.Cue) int {
		switch {
		case a.Start < b.Start:
			return -1
		case a.Start > b.Start:
			return 1
		}
		return 0
	})
	return cues, nil
}

// audioError is the error the user sees when the audio stage failed.
func audioError(err error) error {
	var pe *audiodirector.ProviderError
	switch {
	case errors.Is(err, audiodirector.ErrNoAPIKey):
		return &AIError{Reason: "ElevenLabs の API キーが設定されていません（設定画面で elevenlabs のキーを登録してください）", Err: err}
	case errors.As(err, &pe) && pe.Status == 429:
		return &AIError{Reason: "ElevenLabs のレート制限に達しました。しばらく待ってから再実行してください", Err: err}
	case errors.As(err, &pe):
		return &AIError{Reason: "ElevenLabs の音声合成に失敗しました", Err: err}
	case errors.Is(err, media.ErrUnavailable):
		return ErrUnavailable
	}
	return &AIError{Reason: "音声のミキシングに失敗しました: " + err.Error(), Err: err}
}

// FinalQC runs Final QC on the latest Edit and Audio Timelines (POST /projects/:id/final-qc). The
// Project must be in AUDIO. PASS moves it to READY (through the Final checkpoint when the
// Confirmation Level asks for it); FAIL sends it back to the stage that has the problem.
func (s *EditorService) FinalQC(ctx context.Context, projectID string) (EditorView, error) {
	release, err := s.acquire(projectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if project.State != domain.StateAudio && project.State != domain.StateFinalQC {
		return EditorView{}, conflict("Final QC を実行できるのは音声を作った後（AUDIO）です（現在: %s）", project.State)
	}
	edit, err := s.editors.LatestEditTimeline(ctx, projectID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	audio, err := s.editors.LatestAudioTimeline(ctx, projectID)
	if errors.Is(err, repository.ErrNotFound) {
		return EditorView{}, conflict("音声がまだありません。先に音声を作成してください")
	}
	if err != nil {
		return EditorView{}, err
	}
	in := finalqc.Input{Edit: edit, Audio: audio, MixFileExists: s.fileExists(audio.Timeline.MixPath)}
	for i, c := range edit.Timeline.Clips {
		if _, ok, err := s.takeSource(ctx, c.TakeID); err != nil {
			return EditorView{}, err
		} else if !ok {
			in.MissingClips = append(in.MissingClips, i+1)
		}
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateFinalQC); err != nil {
		return EditorView{}, err
	}
	findings := finalqc.Check(in)
	verdict := domain.FinalQCPass
	if len(findings) > 0 {
		verdict = domain.FinalQCFail
	}
	res, err := s.editors.SaveFinalQC(ctx, projectID, audio.ID, verdict, findings)
	if err != nil {
		s.backTo(ctx, projectID, domain.StateAudio)
		return EditorView{}, err
	}
	if res.Verdict == domain.FinalQCFail {
		stage := finalqc.ReturnStage(findings)
		if err := s.projects.Transition(ctx, projectID, stage); err != nil {
			return EditorView{}, err
		}
		s.announce(ctx, projectID, fmt.Sprintf("Final QC: FAIL。%s に戻します。\n%s", stageLabel(stage), describeQC(findings)))
		return s.View(ctx, projectID)
	}
	summary := fmt.Sprintf("Final QC に合格しました（%.1f 秒 / %.1f LUFS）。書き出してよいですか？", edit.Timeline.Duration(), audio.Timeline.MeasuredLUFS)
	posted, err := s.chatSvc.RequestApproval(ctx, projectID, domain.CheckpointFinal, summary, res.ID)
	if err != nil {
		s.backTo(ctx, projectID, domain.StateAudio)
		return EditorView{}, err
	}
	if !posted {
		if err := s.projects.Transition(ctx, projectID, domain.StateReady); err != nil {
			return EditorView{}, err
		}
		s.announce(ctx, projectID, "Final QC: PASS。書き出しの準備ができました。")
	}
	return s.View(ctx, projectID)
}

func stageLabel(state domain.ProjectState) string {
	if state == domain.StateEditing {
		return "編集（EDITING）"
	}
	return "音声（AUDIO）"
}

func describeQC(findings []domain.QCFinding) string {
	lines := make([]string, len(findings))
	for i, f := range findings {
		lines[i] = "・" + f.Message
	}
	return strings.Join(lines, "\n")
}

// ExportInput is what the user chooses for an export; an empty field takes the default (the
// Brief's aspect ratio, 1080p, H.264).
type ExportInput struct {
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
	_, opts, err := s.briefOptions(ctx, projectID)
	if err != nil {
		return EditorView{}, err
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
		return EditorView{}, invalid("aspect_ratio", "アスペクト比は %s から選んでください", strings.Join(domain.ExportAspectRatios(), " / "))
	}
	if !slices.Contains(domain.ExportResolutions(), opts.Resolution) {
		return EditorView{}, invalid("resolution", "解像度は %s から選んでください", strings.Join(domain.ExportResolutions(), " / "))
	}
	if !slices.Contains(domain.ExportCodecs(), opts.Codec) {
		return EditorView{}, invalid("codec", "コーデックは %s から選んでください", strings.Join(domain.ExportCodecs(), " / "))
	}
	w, h, err := opts.Size()
	if err != nil {
		return EditorView{}, invalid("", "%s", err.Error())
	}
	edit, err := s.editors.LatestEditTimeline(ctx, projectID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	audio, err := s.editors.LatestAudioTimeline(ctx, projectID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	cuts, err := s.cuts(ctx, edit.Timeline)
	if err != nil {
		return EditorView{}, err
	}
	ro := media.RenderOptions{Width: w, Height: h, Codec: opts.Codec}
	if audio.Timeline.MixPath != "" {
		if !s.fileExists(audio.Timeline.MixPath) {
			return EditorView{}, conflict("ミックス済みの音声ファイルが見つかりません。音声を作り直して Final QC をやり直してください")
		}
		ro.Audio = s.assetPath(audio.Timeline.MixPath)
	}
	id := domain.NewID()
	rel := path.Join(projectID, "exports", id+"."+domain.ExportExtension(opts.Codec))
	dst := s.assetPath(rel)
	if _, err := s.tools.Render(ctx, cuts, ro, dst); err != nil {
		return EditorView{}, &AIError{Reason: "動画の書き出しに失敗しました: " + err.Error(), Err: err}
	}
	if _, err := s.editors.CreateExport(context.WithoutCancel(ctx), domain.Export{
		ID: id, ProjectID: projectID, FilePath: rel, Resolution: opts.Resolution, AspectRatio: opts.AspectRatio, Codec: opts.Codec,
	}); err != nil {
		_ = os.Remove(dst)
		return EditorView{}, notFound(err)
	}
	if project.State == domain.StateReady {
		if err := s.projects.Transition(ctx, projectID, domain.StateExported); err != nil {
			return EditorView{}, err
		}
	}
	s.announce(ctx, projectID, fmt.Sprintf("書き出しが完了しました（%s / %s / %s）。ダウンロード: /projects/%s/exports/%s/video", opts.AspectRatio, opts.Resolution, opts.Codec, projectID, id))
	return s.View(ctx, projectID)
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
