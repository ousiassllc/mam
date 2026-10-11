package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/editor"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// JudgeTimeline runs the Editor Judge on the latest Edit Timeline (POST /edit-timelines/:id/judge).
// PASS moves the Project on to AUDIO (through the Rough Cut checkpoint when the Confirmation
// Level asks for it); Re-edit keeps it in EDITING with the findings in the chat.
func (s *EditorService) JudgeTimeline(ctx context.Context, id string) (EditorView, error) {
	ver, err := s.editors.GetEditTimeline(ctx, id)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	release, err := s.acquire(ver.ProjectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, ver.ProjectID)
	if err != nil {
		return EditorView{}, err
	}
	if project, err = s.readyToJudge(ctx, project, ver); err != nil {
		return EditorView{}, err
	}
	res, err := s.runJudge(ctx, project, ver)
	if err != nil {
		return EditorView{}, err
	}
	return s.afterJudge(ctx, project, ver, res)
}

// readyToJudge checks that the version is the latest and the Project is in EDITING; an Escalation
// of the Reshoot Loop that was answered (REPAIRING) continues there, in editing.
func (s *EditorService) readyToJudge(ctx context.Context, project domain.Project, ver domain.EditTimelineVersion) (domain.Project, error) {
	if latest, err := s.editors.LatestEditTimeline(ctx, project.ID, ver.BranchID); err != nil {
		return project, notFound(err)
	} else if latest.ID != ver.ID {
		return project, conflict("この編集より新しい版があります。画面を更新してから判定してください")
	}
	if project.State == domain.StateRepairing {
		if err := s.toEditing(ctx, project); err != nil {
			return project, err
		}
		project.State = domain.StateEditing
	}
	if project.State != domain.StateEditing && project.State != domain.StateEditJudging {
		return project, conflict("編集中（EDITING）の Project だけ判定できます（現在: %s）", project.State)
	}
	return project, nil
}

// runJudge moves the Project to EDIT_JUDGING, runs the Editor Judge and stores its result. When the
// Judge fails the Project goes back to EDITING.
func (s *EditorService) runJudge(ctx context.Context, project domain.Project, ver domain.EditTimelineVersion) (domain.EditorJudgeResult, error) {
	brief, _, err := s.briefOptions(ctx, project.ID)
	if err != nil {
		return domain.EditorJudgeResult{}, err
	}
	idx, err := s.shotIndex(ctx, project.ID, ver.BranchID)
	if err != nil {
		return domain.EditorJudgeResult{}, err
	}
	facts, err := s.shotFacts(ctx, idx, ver.Timeline)
	if err != nil {
		return domain.EditorJudgeResult{}, err
	}
	if err := s.projects.Transition(ctx, project.ID, domain.StateEditJudging); err != nil {
		return domain.EditorJudgeResult{}, err
	}
	verdict, findings, err := s.judge.Judge(ctx, editor.JudgeInput{Brief: brief, Template: project.ProductionTemplate, Timeline: ver.Timeline, Shots: facts})
	if err != nil {
		s.backTo(ctx, project.ID, domain.StateEditing)
		return domain.EditorJudgeResult{}, aiError(err)
	}
	res, err := s.editors.SaveEditorJudge(ctx, ver.ID, verdict, findings)
	if err != nil {
		s.backTo(ctx, project.ID, domain.StateEditing)
		return domain.EditorJudgeResult{}, err
	}
	return res, nil
}

// afterJudge carries out the verdict: NEED_SHOT asks the Director (AUTO) or tells the user,
// Re-edit goes back to EDITING, PASS goes on to AUDIO (through the Rough Cut checkpoint). A state
// change that fails puts the Project back in EDITING.
func (s *EditorService) afterJudge(ctx context.Context, project domain.Project, ver domain.EditTimelineVersion, res domain.EditorJudgeResult) (EditorView, error) {
	projectID := project.ID
	switch res.Verdict {
	case domain.EditorNeedShot:
		auto := project.Mode == domain.ModeAuto && s.generation.gen != nil
		next := "編集画面の「追加 Shot を要求」から Director に追加 Shot を要求してください。"
		if auto {
			next = "Director に追加 Shot を要求します。"
		}
		s.announce(ctx, projectID, "Editor Judge: 素材不足（編集では直せません）\n"+describeFindings(res.Findings)+"\n"+next)
		if auto {
			return s.requestShots(ctx, ver, res)
		}
	case domain.EditorReEdit:
		if err := s.projects.Transition(ctx, projectID, domain.StateEditing); err != nil {
			return EditorView{}, err
		}
		s.announce(ctx, projectID, "Editor Judge: Re-edit（編集で直せる問題があります）\n"+describeFindings(res.Findings))
	default:
		if err := s.passRoughCut(ctx, projectID, ver); err != nil {
			return EditorView{}, err
		}
	}
	return s.View(ctx, projectID, ver.BranchID)
}

// passRoughCut is the PASS verdict: the Rough Cut checkpoint card, or AUDIO at once.
func (s *EditorService) passRoughCut(ctx context.Context, projectID string, ver domain.EditTimelineVersion) error {
	summary := fmt.Sprintf("Rough Cut（%d クリップ / %.1f 秒）が Editor Judge に合格しました。音声制作に進んでよいですか？", len(ver.Timeline.Clips), ver.Timeline.Duration())
	posted, err := s.chatSvc.RequestApproval(ctx, projectID, domain.CheckpointRoughCut, summary, ver.ID)
	if err != nil {
		s.backTo(ctx, projectID, domain.StateEditing)
		return err
	}
	if posted {
		return nil
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateAudio); err != nil {
		s.backTo(ctx, projectID, domain.StateEditing)
		return err
	}
	s.announce(ctx, projectID, "Editor Judge: PASS。音声制作に進みます。")
	return nil
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
