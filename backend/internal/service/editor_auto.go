package service

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// autoStages bounds one run: Rough Cut + Editor Judge, audio, Final QC.
const autoStages = 3

// AdvanceAuto carries an AUTO Project through the editing half of the pipeline: once every Shot of
// the trunk has its Selected Take it makes the Rough Cut and runs the Editor Judge; a Project in
// AUDIO (the Editor Judge passed, or the Rough Cut card was approved) gets its audio and then Final
// QC (spec/requirements/functional.md UC-1). The chain ends at READY, where the export is the
// user's action, and it stops at whatever needs a person: an Approval card, an Editor Judge
// verdict of Re-edit, a Final QC FAIL or a stage that cannot run. A stage never repeats for the
// same input, so a verdict that failed is not retried in a loop.
//
// It runs whenever a job finished, a Take was selected or an Approval card was answered, and does
// nothing for a DIRECTOR Project or a Project that has nothing to do yet.
func (s *EditorService) AdvanceAuto(ctx context.Context, projectID string) {
	mu, _ := s.auto.LoadOrStore(projectID, &sync.Mutex{})
	l := mu.(*sync.Mutex)
	if !l.TryLock() {
		return // a run is going on; it looks at the Project again when it ends
	}
	defer l.Unlock()
	ctx = context.WithoutCancel(ctx)
	for range autoStages {
		progressed, err := s.autoStep(ctx, projectID)
		switch {
		case err == nil && progressed:
			continue
		case err == nil, errors.Is(err, ErrStageBusy), errors.Is(err, ErrNotFound):
		case errors.Is(err, ErrUnavailable):
			s.logger.Warn("AUTO editing stage is unavailable", "project_id", projectID, "err", err)
		default:
			s.logger.Error("AUTO editing stage failed", "project_id", projectID, "err", err)
			s.announce(ctx, projectID, fmt.Sprintf("自動の編集工程が止まりました: %s\n編集画面から続きを実行してください。", err))
		}
		return
	}
}

// autoStep runs the stage the Project is waiting for; it reports whether a stage ran.
func (s *EditorService) autoStep(ctx context.Context, projectID string) (bool, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil || project.Mode != domain.ModeAuto {
		return false, err
	}
	switch project.State {
	case domain.StateVisualDesign, domain.StateGenerating, domain.StateJudging:
		return s.autoRoughCut(ctx, projectID)
	case domain.StateAudio:
		return s.autoAudioOrQC(ctx, projectID)
	}
	return false, nil
}

// autoRoughCut makes the first Rough Cut of the trunk and has it judged, when every Shot has a
// Selected Take with a file. A trunk that already has an Edit Timeline (a Reshoot round is being
// made, the user went back to a Shot) is not cut again.
func (s *EditorService) autoRoughCut(ctx context.Context, projectID string) (bool, error) {
	if _, err := s.editors.LatestEditTimeline(ctx, projectID, ""); err == nil {
		return false, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return false, err
	}
	idx, err := s.shotIndex(ctx, projectID, "")
	if err != nil || len(idx.order) == 0 {
		return false, err
	}
	sources, err := s.loadSources(ctx, selectedTakeIDs(idx.order))
	if err != nil {
		return false, err
	}
	for _, r := range idx.order {
		if _, ok := sources.selected(ctx, r.Shot); !ok {
			return false, nil
		}
	}
	if _, err := s.RoughCut(ctx, projectID, ""); err != nil {
		return false, err
	}
	ver, err := s.editors.LatestEditTimeline(ctx, projectID, "")
	if err != nil {
		return false, notFound(err)
	}
	if _, err := s.JudgeTimeline(ctx, ver.ID); err != nil {
		return false, err
	}
	return true, nil
}

// autoAudioOrQC makes the audio of the latest Edit Timeline when it has none, else runs Final QC on
// the latest audio when that has no result yet.
func (s *EditorService) autoAudioOrQC(ctx context.Context, projectID string) (bool, error) {
	edit, err := s.editors.LatestEditTimeline(ctx, projectID, "")
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	audio, err := s.editors.LatestAudioTimeline(ctx, projectID, "")
	if errors.Is(err, repository.ErrNotFound) || err == nil && audio.Timeline.EditTimelineID != edit.ID {
		if _, err := s.GenerateAudio(ctx, projectID, ""); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	qc, err := s.editors.LatestFinalQC(ctx, projectID)
	if err == nil && qc.AudioTimelineVersionID == audio.ID {
		return false, nil // judged already: a FAIL waits for the user
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return false, err
	}
	if _, err := s.FinalQC(ctx, projectID); err != nil {
		return false, err
	}
	return true, nil
}

// resumeAuto advances every AUTO Project that may be waiting for an editing stage. It runs whenever
// a job finished or a Take was selected: the last Shot of a Project may just have got its Take.
func (s *EditorService) resumeAuto(ctx context.Context) {
	projects, err := s.projects.List(ctx)
	if err != nil {
		s.logger.Error("list projects for the AUTO editing chain failed", "err", err)
		return
	}
	for _, p := range projects {
		if p.Mode == domain.ModeAuto && autoWaiting(p.State) {
			s.AdvanceAuto(ctx, p.ID)
		}
	}
}

// autoWaiting tells the states in which AdvanceAuto has something to start.
func autoWaiting(state domain.ProjectState) bool {
	switch state {
	case domain.StateVisualDesign, domain.StateGenerating, domain.StateJudging, domain.StateAudio:
		return true
	}
	return false
}
