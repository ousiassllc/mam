package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/editor"
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
	auto sync.Map
	// autoStopped is the input (autoInput) each Project's AUTO stage last failed on: project ID -> string.
	autoStopped sync.Map
	preview     sync.Map // preview file -> *sync.Mutex
	// resume is the single worker behind ResumeEditing; missed marks Projects whose Reshoot round
	// found the stage busy, so the release of the stage wakes the worker again.
	resume *resumer
	missed sync.Map // project ID -> struct{}
}

// acquire claims the Project's editing stage; ErrStageBusy when another one runs.
func (s *EditorService) acquire(projectID string) (release func(), err error) {
	mu, _ := s.stage.LoadOrStore(projectID, &sync.Mutex{})
	l := mu.(*sync.Mutex)
	if !l.TryLock() {
		return nil, ErrStageBusy
	}
	return func() {
		l.Unlock()
		if _, ok := s.missed.LoadAndDelete(projectID); ok {
			s.resume.Wake()
		}
	}, nil
}

// fileExists tells whether the stored path is a regular file inside the assets directory; every
// caller that reads a stored path checks it first, so a path that leaves the directory never reaches
// ffmpeg or the file server.
func (s *EditorService) fileExists(rel string) bool {
	abs := s.assetPath(rel)
	if abs == "" {
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

// shotIndex names every Shot of the series ("Scene 1 · Shot 2") and remembers what the clips need.
// Scenes are numbered by their place in the story: a Branch's own Scenes count from 0 again, so
// Scene.OrderIndex alone cannot tell them from the Scenes it inherits.
type shotIndex struct {
	byID   map[string]repository.ShotRef
	labels map[string]string
	// scene is the 0-based place of the Shot's Scene in the story (by shot ID).
	scene map[string]int
	order []repository.ShotRef
}

func (s *EditorService) shotIndex(ctx context.Context, projectID, branchID string) (shotIndex, error) {
	refs, err := s.shots.StoryShots(ctx, projectID, branchID)
	if err != nil {
		return shotIndex{}, notFound(err)
	}
	idx := shotIndex{byID: make(map[string]repository.ShotRef, len(refs)), labels: make(map[string]string, len(refs)), scene: make(map[string]int, len(refs)), order: refs}
	place, lastScene := -1, ""
	for _, r := range refs {
		if r.Scene.ID != lastScene {
			place, lastScene = place+1, r.Scene.ID
		}
		idx.byID[r.Shot.ID] = r
		idx.scene[r.Shot.ID] = place
		idx.labels[r.Shot.ID] = fmt.Sprintf("Scene %d · Shot %d", place+1, r.Shot.OrderIndex+1)
	}
	return idx, nil
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

// announce posts a message of the editing stage in the chat. A failure is logged, not returned:
// the stage has been done.
func (s *EditorService) announce(ctx context.Context, projectID, text string) {
	if err := s.chatSvc.Announce(context.WithoutCancel(ctx), projectID, assistantText(text)); err != nil {
		s.logger.Error("post editor notice failed", "project_id", projectID, "err", err)
	}
}

// backTo puts the Project back into a state after a stage failed. It works on a context that
// survives the request, and only logs: the failure being reported is the caller's.
func (s *EditorService) backTo(ctx context.Context, projectID string, state domain.ProjectState) {
	if err := s.projects.Transition(context.WithoutCancel(ctx), projectID, state); err != nil {
		s.logger.Error("restore project state failed", "project_id", projectID, "state", state, "err", err)
	}
}
