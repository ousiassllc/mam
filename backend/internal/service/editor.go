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
	// generation makes the additional Shots the Reshoot Loop asks for; policies hold the Retry
	// Policy that limits its rounds.
	generation *GenerationService
	policies   *repository.PolicyRepository
	takes      *repository.TakeRepository
	// locks give the Editing setting of a Shot and the Music setting of the Project (spec/spec.md §6).
	locks *repository.LockRepository
	judge *editor.Judge
	// brands and judges give Final QC the Brand constraints and the Brand Issues of the Takes (F-23).
	brands *repository.BrandRepository
	judges *repository.JudgeRepository
	// audio is nil without ffmpeg or the speech vendor; cloner is nil without the ElevenLabs adapter.
	audio   *audiodirector.Director
	voices  *repository.VoiceRepository
	cloner  audiodirector.VoiceCloner
	chatSvc *ChatService
	// tools is ffmpeg / ffprobe; nil when they are not installed.
	tools *media.Tools
	stage sync.Map // project ID -> *sync.Mutex
	// auto guards the AUTO chain of a Project (AdvanceAuto): project ID -> *sync.Mutex.
	auto    sync.Map
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

// assetPath resolves an asset-relative path inside the assets directory. A path that leaves it
// (a stored "../x") resolves to "", which fileExists rejects: every caller that reads a stored
// path checks fileExists first, so such a file never reaches ffmpeg or the file server.
func (s *EditorService) assetPath(rel string) string {
	abs := filepath.Join(s.assetsDir, filepath.FromSlash(rel))
	if within, err := filepath.Rel(s.assetsDir, abs); err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return ""
	}
	return abs
}

func (s *EditorService) fileExists(rel string) bool {
	abs := s.assetPath(rel)
	if rel == "" || abs == "" {
		return false
	}
	st, err := os.Stat(abs)
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
	case domain.StateEditing, domain.StateVisualDesign, domain.StateGenerating, domain.StateJudging, domain.StateRepairing,
		domain.StateEditJudging, domain.StateAudio, domain.StateFinalQC:
		return true
	}
	return false
}

// EditorView is the editing state of a Project: what the panel shows.
type EditorView struct {
	Project domain.Project
	// BranchID is the Branch whose Edit Timeline is shown ("" for the trunk).
	BranchID string
	// PendingShots is how many additional Shots of the Editor → Reshoot Loop are still being made.
	PendingShots int
	Timeline     *TimelineView
	Audio        *domain.AudioTimelineVersion
	// Voices are the cloned voices of the Project's speakers.
	Voices  []domain.VoiceClone
	QC      *domain.FinalQCResult
	Exports []domain.Export
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

// View returns the editing state of the Project: the Edit Timeline of its trunk (branchID "") or
// of one of its Branches (GET /projects/:id/edit).
func (s *EditorService) View(ctx context.Context, projectID, branchID string) (EditorView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	_, defaults, err := s.briefOptions(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	v := EditorView{Project: project, BranchID: branchID, Defaults: defaults, CanRender: s.tools != nil}
	pending, err := s.editors.PendingReshoots(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	for _, r := range pending {
		if r.BranchID == branchID {
			v.PendingShots++
		}
	}
	if tl, err := s.Timeline(ctx, projectID, branchID); err == nil {
		v.Timeline = &tl
	} else if !errors.Is(err, ErrNotFound) {
		return EditorView{}, err
	}
	if a, err := s.editors.LatestAudioTimeline(ctx, projectID, branchID); err == nil {
		v.Audio = &a
	} else if !errors.Is(err, repository.ErrNotFound) {
		return EditorView{}, err
	}
	if v.Voices, err = s.voices.List(ctx, projectID); err != nil {
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

func (s *EditorService) shotIndex(ctx context.Context, projectID, branchID string) (shotIndex, error) {
	refs, err := s.shots.StoryShots(ctx, projectID, branchID)
	if err != nil {
		return shotIndex{}, notFound(err)
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
func (s *EditorService) Timeline(ctx context.Context, projectID, branchID string) (TimelineView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return TimelineView{}, err
	}
	ver, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if err != nil {
		return TimelineView{}, notFound(err)
	}
	return s.timelineView(ctx, ver)
}

func (s *EditorService) timelineView(ctx context.Context, ver domain.EditTimelineVersion) (TimelineView, error) {
	idx, err := s.shotIndex(ctx, ver.ProjectID, ver.BranchID)
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
	case domain.StateRepairing:
		path = []domain.ProjectState{domain.StateGenerating, domain.StateJudging, domain.StateEditing}
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

// RoughCut makes the Rough Cut from the Selected Takes of every Shot of the trunk of the Project,
// or of its Branch (POST /projects/:id/edit/rough-cut), and starts EDITING. Every Shot needs a
// Selected Take with a file. The Editor Agent then edits what the Production Template ranks (Pace,
// Hook, CTA Timing, Caption: editor.Advance) and additional Shots the Reshoot Loop already put in
// stay in the cut, after the Shot they follow.
func (s *EditorService) RoughCut(ctx context.Context, projectID, branchID string) (EditorView, error) {
	release, err := s.acquire(projectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	idx, err := s.shotIndex(ctx, projectID, branchID)
	if err != nil {
		return EditorView{}, err
	}
	reshoots, err := s.editors.ListReshoots(ctx, projectID, branchID)
	if err != nil {
		return EditorView{}, err
	}
	additional := make(map[string]bool, len(reshoots))
	for _, r := range reshoots {
		additional[r.ShotID] = true
	}
	var story []repository.ShotRef
	for _, r := range idx.order {
		if !additional[r.Shot.ID] {
			story = append(story, r)
		}
	}
	if len(story) == 0 {
		return EditorView{}, invalid("", "Shot がまだありません。Scene / Shot を作成してから Rough Cut を作ってください")
	}
	cuts := make([]editor.ShotCut, 0, len(story))
	var missing []string
	for _, r := range story {
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
	placed := map[string]bool{}
	for _, r := range reshoots {
		ref, known := idx.byID[r.ShotID]
		if !known || r.Status != domain.ReshootApplied {
			continue
		}
		src, ok, err := s.selectedSource(ctx, ref.Shot)
		if err != nil {
			return EditorView{}, err
		}
		if !ok {
			continue // the additional Shot has no usable Take now: the cut goes on without it
		}
		at := insertionPoint(len(cuts), func(i int) string { return cuts[i].ShotID }, r.AfterShotID, placed)
		c := editor.ShotCut{ShotID: r.ShotID, TakeID: src.takeID, Planned: r.DurationSeconds, TakeSeconds: src.seconds, Kind: r.Kind}
		if at > 0 {
			c.SceneIndex = cuts[at-1].SceneIndex
		}
		cuts = slices.Insert(cuts, at, c)
		placed[r.ShotID] = true
	}
	tl, err := editor.RoughCut(cuts).Normalize()
	if err != nil {
		return EditorView{}, invalid("", "%s", timelineMessage(err))
	}
	if tl, err = s.keepFixedClips(ctx, idx, projectID, branchID, tl); err != nil {
		return EditorView{}, err
	}
	brief, _, err := s.briefOptions(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	facts, err := s.shotFacts(ctx, idx, tl)
	if err != nil {
		return EditorView{}, err
	}
	tl, notes := editor.Advance(editor.JudgeInput{Brief: brief, Template: project.ProductionTemplate, Timeline: tl, Shots: facts})
	if tl, err = tl.Normalize(); err != nil {
		return EditorView{}, invalid("", "%s", timelineMessage(err))
	}
	if err := s.toEditing(ctx, project); err != nil {
		return EditorView{}, err
	}
	if _, err := s.editors.SaveEditTimeline(ctx, projectID, branchID, "", tl); err != nil {
		return EditorView{}, notFound(err)
	}
	text := fmt.Sprintf("Rough Cut を作成しました（%d クリップ / %.1f 秒）。タイムラインで並べ替え・Cut Point・Transition・字幕を編集できます。", len(tl.Clips), tl.Duration())
	if len(notes) > 0 {
		text += fmt.Sprintf("\nProduction Template（%s）に合わせて編集しました:\n・%s", project.ProductionTemplate.Label(), strings.Join(notes, "\n・"))
	}
	s.announce(ctx, projectID, text)
	return s.View(ctx, projectID, branchID)
}

// keepFixedClips puts back, into a Rough Cut that is made again, the clips of the Shots whose
// Editing is MANUAL or LOCK (spec/spec.md §6): the AI does not re-cut what the user cut. A clip
// whose Take file is gone is not brought back, since it could not be played.
func (s *EditorService) keepFixedClips(ctx context.Context, idx shotIndex, projectID, branchID string, fresh domain.EditTimeline) (domain.EditTimeline, error) {
	fixed, err := s.editingFixed(ctx, idx, fresh)
	if err != nil || len(fixed) == 0 {
		return fresh, err
	}
	prev, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if errors.Is(err, repository.ErrNotFound) {
		return fresh, nil
	}
	if err != nil {
		return domain.EditTimeline{}, err
	}
	usable := make(map[string]bool, len(fixed))
	for _, c := range prev.Timeline.Clips {
		if !fixed[c.ShotID] {
			continue
		}
		if _, ok, err := s.takeSource(ctx, c.TakeID); err != nil {
			return domain.EditTimeline{}, err
		} else if ok {
			usable[c.ShotID] = true
		}
	}
	kept, err := editor.KeepFixedClips(fresh, prev.Timeline, usable).Normalize()
	if err != nil {
		return domain.EditTimeline{}, invalid("", "%s", timelineMessage(err))
	}
	return kept, nil
}

// editingFixed lists the Shots of the timeline whose Editing parameter is MANUAL or LOCK.
func (s *EditorService) editingFixed(ctx context.Context, idx shotIndex, tl domain.EditTimeline) (map[string]bool, error) {
	fixed := map[string]bool{}
	for _, c := range tl.Clips {
		if _, ok := idx.byID[c.ShotID]; !ok || fixed[c.ShotID] {
			continue
		}
		locks, err := s.locks.ShotLocks(ctx, c.ShotID)
		if err != nil {
			return nil, err
		}
		if locks.Mode(domain.ParamEditing) != domain.LockAuto {
			fixed[c.ShotID] = true
		}
	}
	return fixed, nil
}

// insertionPoint is where an additional Shot that follows the Shot anchor goes into a list of n
// clips: right after the anchor and the additional Shots already placed there (the end when the
// anchor is not in the list).
func insertionPoint(n int, shotAt func(i int) string, anchor string, placed map[string]bool) int {
	at := n
	for i := range n {
		if shotAt(i) == anchor {
			at = i + 1
		}
	}
	for at < n && placed[shotAt(at)] {
		at++
	}
	return at
}

// timelineMessage is the message of an invalid timeline without its sentinel prefix.
func timelineMessage(err error) string {
	return strings.TrimPrefix(err.Error(), domain.ErrInvalidTimeline.Error()+": ")
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
	idx, err := s.shotIndex(ctx, project.ID, base.BranchID)
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
	if latest, err := s.editors.LatestEditTimeline(ctx, project.ID, base.BranchID); err != nil {
		return TimelineView{}, notFound(err)
	} else if latest.ID != id {
		return TimelineView{}, conflict("別の編集が保存されています。画面を更新してから編集し直してください")
	}
	if err := s.toEditing(ctx, project); err != nil {
		return TimelineView{}, err
	}
	ver, err := s.editors.SaveEditTimeline(ctx, project.ID, base.BranchID, id, norm)
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
	fixed, err := s.editingFixed(ctx, idx, tl)
	if err != nil {
		return nil, err
	}
	for _, c := range tl.Clips {
		r, ok := idx.byID[c.ShotID]
		if !ok {
			continue
		}
		f := editor.ShotFacts{Label: idx.labels[c.ShotID], Objective: r.Shot.Objective, SelectedTakeID: r.Shot.SelectedTakeID, EditingFixed: fixed[c.ShotID]}
		if sb, ok := r.Shot.Storyboard(); ok {
			f.Dialogue = sb.Dialogue
		}
		if spec, ok := r.Shot.CanonicalSpec(); ok {
			f.HasProduct = spec.Product != nil
		}
		if _, f.TakeReady, err = s.takeSource(ctx, c.TakeID); err != nil {
			return nil, err // a failed read is not a missing file: the Editor Judge must not report one
		}
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
	if latest, err := s.editors.LatestEditTimeline(ctx, projectID, ver.BranchID); err != nil {
		return EditorView{}, notFound(err)
	} else if latest.ID != id {
		return EditorView{}, conflict("この編集より新しい版があります。画面を更新してから判定してください")
	}
	if project.State == domain.StateRepairing {
		// An Escalation card of the Reshoot Loop was answered: the Project continues here, in editing.
		if err := s.toEditing(ctx, project); err != nil {
			return EditorView{}, err
		}
		project.State = domain.StateEditing
	}
	if project.State != domain.StateEditing && project.State != domain.StateEditJudging {
		return EditorView{}, conflict("編集中（EDITING）の Project だけ判定できます（現在: %s）", project.State)
	}
	brief, _, err := s.briefOptions(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	idx, err := s.shotIndex(ctx, projectID, ver.BranchID)
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
	switch res.Verdict {
	case domain.EditorNeedShot:
		next := "編集画面の「追加 Shot を要求」から Director に追加 Shot を要求してください。"
		if project.Mode == domain.ModeAuto && s.generation.gen != nil {
			next = "Director に追加 Shot を要求します。"
		}
		s.announce(ctx, projectID, "Editor Judge: 素材不足（編集では直せません）\n"+describeFindings(res.Findings)+"\n"+next)
		if project.Mode == domain.ModeAuto && s.generation.gen != nil {
			return s.requestShots(ctx, ver, res)
		}
		return s.View(ctx, projectID, ver.BranchID)
	case domain.EditorReEdit:
		if err := s.projects.Transition(ctx, projectID, domain.StateEditing); err != nil {
			return EditorView{}, err
		}
		s.announce(ctx, projectID, "Editor Judge: Re-edit（編集で直せる問題があります）\n"+describeFindings(res.Findings))
		return s.View(ctx, projectID, ver.BranchID)
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
	return s.View(ctx, projectID, ver.BranchID)
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
	cuts := make([]media.Cut, len(tl.Clips))
	for i, c := range tl.Clips {
		src, ok, err := s.takeSource(ctx, c.TakeID)
		if err != nil {
			return nil, err
		}
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
	edit, err := s.editors.LatestEditTimeline(ctx, projectID, "")
	if err != nil {
		return EditorView{}, notFound(err)
	}
	audio, err := s.editors.LatestAudioTimeline(ctx, projectID, "")
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
	if err := s.brandInput(ctx, projectID, &in); err != nil {
		return EditorView{}, err
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateFinalQC); err != nil {
		return EditorView{}, err
	}
	findings := finalqc.Check(in)
	verdict := domain.FinalQCPass
	if finalqc.Failed(findings) {
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
		return s.View(ctx, projectID, "")
	}
	summary := fmt.Sprintf("Final QC に合格しました（%.1f 秒 / %.1f LUFS）。書き出してよいですか？", edit.Timeline.Duration(), audio.Timeline.MeasuredLUFS)
	if len(findings) > 0 {
		summary += "\n" + describeQC(findings)
	}
	posted, err := s.chatSvc.RequestApproval(ctx, projectID, domain.CheckpointFinal, summary, res.ID)
	if err != nil {
		s.backTo(ctx, projectID, domain.StateAudio)
		return EditorView{}, err
	}
	if !posted {
		if err := s.projects.Transition(ctx, projectID, domain.StateReady); err != nil {
			return EditorView{}, err
		}
		text := "Final QC: PASS。書き出しの準備ができました。"
		if len(findings) > 0 {
			text += "\n" + describeQC(findings)
		}
		s.announce(ctx, projectID, text)
	}
	return s.View(ctx, projectID, "")
}

// brandInput gives Final QC the Brand / Safety Constraints, the lines the deliverable speaks, the
// texts of the Shot Specs of its clips and the Brand Issues the AI Video Judge found in their Takes.
// Issues of a constraint that was deleted since are not counted.
func (s *EditorService) brandInput(ctx context.Context, projectID string, in *finalqc.Input) error {
	brand, err := s.brands.List(ctx, projectID)
	if err != nil {
		return err
	}
	if len(brand) == 0 {
		return nil
	}
	in.Brand = brand
	idx, err := s.shotIndex(ctx, projectID, "")
	if err != nil {
		return err
	}
	scenes, err := s.audioScenes(ctx, projectID, "", in.Edit.Timeline)
	if err != nil {
		return err
	}
	for _, sc := range scenes {
		for _, line := range sc.Lines {
			if vl, ok := domain.ParseVoiceLine(line); ok {
				in.Spoken = append(in.Spoken, vl.Text)
			}
		}
	}
	for i, c := range in.Edit.Timeline.Clips {
		if r, ok := idx.byID[c.ShotID]; ok {
			if spec, ok := r.Shot.CanonicalSpec(); ok {
				in.ShotTexts = append(in.ShotTexts, spec.BrandText())
			}
		}
		res, err := s.judges.Latest(ctx, c.TakeID)
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		for _, is := range res.Issues {
			if _, ok := brand.ByID(is.BrandConstraintID); ok {
				in.ClipBrandIssues = append(in.ClipBrandIssues, finalqc.ClipBrandIssue{Clip: i + 1, Issue: is})
			}
		}
	}
	return nil
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
	edit, err := s.editors.LatestEditTimeline(ctx, projectID, in.BranchID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	audio, err := s.editors.LatestAudioTimeline(ctx, projectID, in.BranchID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	cuts, err := s.cuts(ctx, edit.Timeline, audio.Timeline.LipSync)
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
		ID: id, ProjectID: projectID, BranchID: in.BranchID, FilePath: rel, Resolution: opts.Resolution, AspectRatio: opts.AspectRatio, Codec: opts.Codec,
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
	return s.View(ctx, projectID, "")
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

// RequestAdditionalShot asks the Director for the additional Shots the Editor Judge of the Edit
// Timeline version found missing (POST /edit-timelines/:id/request-additional-shot, F-19): it adds
// each as a Shot of the Project, records the request as the next round of the Editor → Reshoot
// Loop and starts generating them. The Project goes to GENERATING; once every Shot of the round
// has its Selected Take they are put into the Edit Timeline and the Project is back in EDITING
// (ResumeReshoots). The rounds are limited by the Retry Policy, shared with the Repair / Retake Loop.
func (s *EditorService) RequestAdditionalShot(ctx context.Context, id string) (EditorView, error) {
	ver, err := s.editors.GetEditTimeline(ctx, id)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	release, err := s.acquire(ver.ProjectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()
	res, err := s.editors.LatestEditorJudge(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return EditorView{}, conflict("この版はまだ Editor Judge にかけていません")
	}
	if err != nil {
		return EditorView{}, err
	}
	return s.requestShots(ctx, ver, res)
}

// requestShots is RequestAdditionalShot with the stage held and the Judge result read.
func (s *EditorService) requestShots(ctx context.Context, ver domain.EditTimelineVersion, res domain.EditorJudgeResult) (EditorView, error) {
	projectID, branchID := ver.ProjectID, ver.BranchID
	if res.Verdict != domain.EditorNeedShot {
		return EditorView{}, conflict("この版の Editor Judge は素材不足を判定していません")
	}
	if s.generation == nil || s.generation.gen == nil {
		return EditorView{}, ErrUnavailable
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if latest, err := s.editors.LatestEditTimeline(ctx, projectID, branchID); err != nil {
		return EditorView{}, notFound(err)
	} else if latest.ID != ver.ID {
		return EditorView{}, conflict("この編集より新しい版があります。画面を更新してから Editor Judge をやり直してください")
	}
	if project.State != domain.StateEditJudging {
		return EditorView{}, conflict("追加 Shot を要求できるのは Editor Judge が素材不足と判定した直後（EDIT_JUDGING）です（現在: %s）", project.State)
	}
	pending, err := s.editors.PendingReshoots(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if slices.ContainsFunc(pending, func(r domain.ReshootRequest) bool { return r.BranchID == branchID }) {
		return EditorView{}, conflict("前回の追加 Shot がまだできていません")
	}
	policy, err := s.policies.Get(ctx, projectID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	rounds, err := s.editors.ReshootRounds(ctx, projectID, branchID)
	if err != nil {
		return EditorView{}, err
	}
	if verdict := policy.Effective().CanRetry(rounds, 0, 0, 0); verdict != domain.RetryAllowed {
		return s.escalateReshoot(ctx, ver, res, rounds, verdict)
	}
	idx, err := s.shotIndex(ctx, projectID, branchID)
	if err != nil {
		return EditorView{}, err
	}
	clips := ver.Timeline.Clips
	var (
		reqs       []repository.NewReshoot
		extraScene string
	)
	for _, f := range res.Findings {
		need := f.Need
		if need == nil || !need.Valid(len(clips)) {
			continue
		}
		anchor := clips[need.AfterClip-1]
		ref, ok := idx.byID[anchor.ShotID]
		if !ok {
			continue
		}
		shot := domain.Shot{SceneID: ref.Shot.SceneID, Objective: strings.TrimSpace(need.Objective), Duration: need.Seconds()}
		var newScene *domain.Scene
		if ref.Scene.BranchID != branchID {
			// The Shot follows one of the series the Branch hangs from: it goes into a Scene of the
			// Branch, so the additional Shot never appears in the series it is not part of.
			if extraScene == "" {
				newScene = &domain.Scene{BranchID: branchID, Title: "追加カット"}
			} else {
				shot.SceneID = extraScene
			}
		}
		shot, err = s.plans.AppendContinuation(ctx, projectID, newScene, shot)
		if err != nil {
			return EditorView{}, notFound(err)
		}
		if newScene != nil {
			extraScene = shot.SceneID
		}
		reqs = append(reqs, repository.NewReshoot{ShotID: shot.ID, AfterShotID: anchor.ShotID, Kind: need.Kind, Objective: shot.Objective, DurationSeconds: shot.Duration})
	}
	if len(reqs) == 0 {
		return EditorView{}, conflict("追加できる Shot がありません（判定に追加 Shot の指定がありません）")
	}
	if _, err := s.editors.CreateReshootRound(ctx, ver.ID, reqs); err != nil {
		return EditorView{}, notFound(err)
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateGenerating); err != nil {
		return EditorView{}, err
	}
	lines := make([]string, len(reqs))
	for i, r := range reqs {
		lines[i] = fmt.Sprintf("・%s（%.1f 秒）: %s", clipKindLabel(r.Kind), r.DurationSeconds, r.Objective)
	}
	s.announce(ctx, projectID, fmt.Sprintf("Director に追加 Shot を %d 件要求しました（Editor → Reshoot Loop %d 回目）。生成できたら編集に戻ります。\n%s", len(reqs), rounds+1, strings.Join(lines, "\n")))
	var startErr error
	for _, r := range reqs {
		if _, err := s.generation.gen.Start(ctx, r.ShotID, ""); err != nil && startErr == nil {
			startErr = err
		}
	}
	if startErr != nil {
		return EditorView{}, mapGenerationError(startErr)
	}
	return s.View(ctx, projectID, branchID)
}

func clipKindLabel(k domain.ClipKind) string {
	if k == domain.ClipReaction {
		return "リアクションショット"
	}
	return "インサートショット"
}

// escalateReshoot stops the Editor → Reshoot Loop when the Retry Policy allows no further round and
// asks the user for a decision (an Escalation card, posted at every Confirmation Level).
func (s *EditorService) escalateReshoot(ctx context.Context, ver domain.EditTimelineVersion, res domain.EditorJudgeResult, rounds int, verdict domain.RetryVerdict) (EditorView, error) {
	var missing []domain.EditorFinding
	for _, f := range res.Findings {
		if f.Need != nil {
			missing = append(missing, f)
		}
	}
	text := fmt.Sprintf("追加 Shot の要求を止めました: %s（これまでに %d 回要求しました）。\n足りない素材:\n%s", verdict.Reason(), rounds, describeFindings(missing))
	options := []string{
		"設定で上限を引き上げてから、もう一度 Editor Judge を実行する",
		"追加 Shot なしで、今ある素材のまま編集を続ける",
		"Shot を手動で追加して生成する",
	}
	if err := s.chatSvc.RaiseEscalation(context.WithoutCancel(ctx), ver.ProjectID, text, options, ""); err != nil {
		return EditorView{}, err
	}
	return s.View(ctx, ver.ProjectID, ver.BranchID)
}

// ResumeReshoots is the return of the Editor → Reshoot Loop: it puts the additional Shots that
// have their Selected Take into the Edit Timeline of the trunk / Branch that asked for them and
// takes the Project back to EDITING. It runs whenever a job finished or a Take was selected, and
// does nothing for a round whose Shots are not all ready.
func (s *EditorService) ResumeReshoots(ctx context.Context) {
	ids, err := s.editors.ProjectsWithPendingReshoots(ctx)
	if err != nil {
		s.logger.Error("list projects with pending additional shots failed", "err", err)
		return
	}
	for _, id := range ids {
		if err := s.applyReshoots(ctx, id); err != nil {
			s.logger.Error("put additional shots into the edit timeline failed", "project_id", id, "err", err)
		}
	}
}

func (s *EditorService) applyReshoots(ctx context.Context, projectID string) error {
	release, err := s.acquire(projectID)
	if err != nil {
		return nil // another stage runs; the next job or selection brings us back
	}
	defer release()
	pending, err := s.editors.PendingReshoots(ctx, projectID)
	if err != nil {
		return err
	}
	type round struct {
		branch string
		number int
	}
	groups := map[round][]domain.ReshootRequest{}
	var order []round
	for _, r := range pending {
		k := round{r.BranchID, r.Round}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	for _, k := range order {
		if err := s.applyRound(ctx, projectID, k.branch, groups[k]); err != nil {
			return err
		}
	}
	return nil
}

// applyRound puts the Shots of one round into the latest Edit Timeline of the series, each after
// the Shot it follows, as one new version. A round whose Shots are not all ready waits.
func (s *EditorService) applyRound(ctx context.Context, projectID, branchID string, reqs []domain.ReshootRequest) error {
	idx, err := s.shotIndex(ctx, projectID, branchID)
	if err != nil {
		return err
	}
	sources := make([]cutSource, len(reqs))
	for i, r := range reqs {
		ref, known := idx.byID[r.ShotID]
		if !known {
			return nil
		}
		src, ok, err := s.selectedSource(ctx, ref.Shot)
		if err != nil {
			return err
		}
		if !ok {
			return nil // still being made
		}
		sources[i] = src
	}
	latest, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	clips := slices.Clone(latest.Timeline.Clips)
	placed := map[string]bool{}
	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = r.ID
		if slices.ContainsFunc(clips, func(c domain.EditClip) bool { return c.ShotID == r.ShotID }) {
			continue // a Rough Cut made since already has it
		}
		out := r.DurationSeconds
		if sources[i].seconds > 0 {
			out = min(out, sources[i].seconds)
		}
		clip := domain.EditClip{
			ShotID: r.ShotID, TakeID: sources[i].takeID, Kind: r.Kind, SourceSeconds: sources[i].seconds, Out: out,
			Transition: domain.Transition{Type: domain.TransitionCut},
		}
		at := insertionPoint(len(clips), func(j int) string { return clips[j].ShotID }, r.AfterShotID, placed)
		clips = slices.Insert(clips, at, clip)
		placed[r.ShotID] = true
	}
	tl, err := domain.EditTimeline{Clips: clips}.Normalize()
	if err != nil {
		return fmt.Errorf("additional shots do not fit the timeline: %w", err)
	}
	if _, err := s.editors.SaveReshootApplied(ctx, projectID, branchID, latest.ID, tl, ids); err != nil {
		if errors.Is(err, repository.ErrStale) || errors.Is(err, repository.ErrConflict) {
			return nil // edited or applied meanwhile: the next trigger looks again
		}
		return err
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return err
	}
	s.announce(ctx, projectID, fmt.Sprintf("追加 Shot %d 件を編集タイムラインに入れました。編集に戻ります。タイムラインを確認して、もう一度 Editor Judge を実行してください。", len(reqs)))
	return s.toEditing(ctx, project)
}
