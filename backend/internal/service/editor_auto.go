package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// same input: one that failed is neither retried nor announced again until the user does something
// (RetryAuto: an answered card) or the input changes (a saved timeline, another Selected Take), so
// a job of another Project cannot make it run, and spend, again.
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

// RetryAuto forgets the stage of the Project that failed, because the user did something (answered
// a card): the chain looks at the Project again.
func (s *EditorService) RetryAuto(projectID string) {
	s.autoStopped.Delete(projectID)
	go s.AdvanceAuto(context.Background(), projectID)
}

// autoStep runs the stage the Project is waiting for; it reports whether a stage ran. A stage that
// failed on the same input before does not run again (see AdvanceAuto).
func (s *EditorService) autoStep(ctx context.Context, projectID string) (bool, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil || project.Mode != domain.ModeAuto {
		return false, err
	}
	input, err := s.autoInput(ctx, project)
	if err != nil || input == "" {
		return false, err
	}
	if last, ok := s.autoStopped.Load(projectID); ok && last == input {
		return false, nil
	}
	var progressed bool
	switch project.State {
	case domain.StateAudio:
		progressed, err = s.autoAudioOrQC(ctx, projectID)
	default:
		progressed, err = s.autoRoughCut(ctx, projectID)
	}
	switch {
	case err == nil:
		if progressed {
			s.autoStopped.Delete(projectID)
		}
	case !errors.Is(err, ErrStageBusy) && !errors.Is(err, ErrNotFound):
		s.autoStopped.Store(projectID, input)
	}
	return progressed, err
}

// autoInput identifies what the stage of the Project works on, "" when there is no stage: the
// Selected Takes before the Rough Cut, the latest Edit and Audio Timelines after it.
func (s *EditorService) autoInput(ctx context.Context, project domain.Project) (string, error) {
	switch project.State {
	case domain.StateVisualDesign, domain.StateGenerating, domain.StateJudging:
		idx, err := s.shotIndex(ctx, project.ID, "")
		if err != nil {
			return "", err
		}
		return "cut:" + strings.Join(selectedTakeIDs(idx.order), ","), nil
	case domain.StateAudio:
		edit, err := s.editors.LatestEditTimeline(ctx, project.ID, "")
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		input := "audio:" + edit.ID
		audio, err := s.editors.LatestAudioTimeline(ctx, project.ID, "")
		if err == nil {
			input += ":" + audio.ID
		} else if !errors.Is(err, repository.ErrNotFound) {
			return "", err
		}
		return input, nil
	}
	return "", nil
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
