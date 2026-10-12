package service

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// DirectorCommandResult is what a direction did.
type DirectorCommandResult struct {
	Inspector Inspector
	// Started is true when a regeneration was queued (HTTP 202).
	Started bool
}

// DirectorCommand carries out a natural language direction for a Shot whose target is known
// (POST /shots/:id/director-command). The message and the result are recorded in the thread. It
// fails with ErrLocked, and does nothing, when the direction changes a LOCKed parameter.
func (s *DirectorService) DirectorCommand(ctx context.Context, shotID, message string) (DirectorCommandResult, error) {
	message = strings.TrimSpace(message)
	if err := validateMessage(message, false); err != nil {
		return DirectorCommandResult{}, invalid("content", "指示を入力してください（%d 文字以内）", domain.MaxMessageRunes)
	}
	// A second direction for a Shot (a double click, a second tab) while one is being carried out would pay for another Interpreter call and run it twice.
	key := "director-command\x00" + shotID // answering also holds Shot IDs: the prefix keeps the keys apart
	if _, busy := s.answering.LoadOrStore(key, struct{}{}); busy {
		return DirectorCommandResult{}, ErrConflict
	}
	defer s.answering.Delete(key)
	ref, err := s.gens.GetShot(ctx, shotID)
	if err != nil {
		return DirectorCommandResult{}, notFound(err)
	}
	if s.interpreter == nil {
		return DirectorCommandResult{}, ErrUnavailable
	}
	project, err := s.getProject(ctx, ref.ProjectID)
	if err != nil {
		return DirectorCommandResult{}, err
	}
	shots, err := s.gens.StoryShots(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return DirectorCommandResult{}, err
	}
	cmd, err := s.interpret(ctx, message, shots, &ref)
	if err != nil {
		return DirectorCommandResult{}, interpretError(err)
	}
	locks, err := s.locks.ShotLocks(ctx, shotID)
	if err != nil {
		return DirectorCommandResult{}, err
	}
	if err := lockedRegeneration(locks, cmd.Operations); err != nil {
		return DirectorCommandResult{}, err // nothing is recorded or done
	}
	if err := s.refuseBrand(ctx, project.ID, message, cmd); err != nil {
		return DirectorCommandResult{}, err // a LOCK brand violation is 403; nothing is recorded or queued
	}
	// The direction is committed before execute: execute announces the progress card, which a chat that
	// is open shows at once and then asks only for what comes after it (`after`), so a direction
	// committed later would never reach it.
	user := assignIDs([]domain.ChatMessage{userText(message, "", nil)})
	if err := s.commitMessages(ctx, project.ID, user); err != nil {
		return DirectorCommandResult{}, err
	}
	res, err := s.execute(ctx, project, &ref, cmd, message, true)
	if err != nil {
		s.reportCommandFailure(ctx, project.ID, res, err)
		return DirectorCommandResult{}, err
	}
	// The direction was carried out (a regeneration may be queued and paid for): its answer is recorded
	// whatever happened to the request since (a closed client), as the failure notice is, so that a retry
	// does not run it a second time for want of seeing it in the thread.
	if err := s.commitMessages(context.WithoutCancel(ctx), project.ID, assignIDs([]domain.ChatMessage{assistantText(res.text())})); err != nil {
		return DirectorCommandResult{}, err
	}
	view, err := s.Inspector(ctx, shotID)
	return DirectorCommandResult{Inspector: view, Started: res.started}, err
}

// reportCommandFailure answers a direction that failed after it was recorded (a Planner spec that
// breaks a LOCK Brand constraint, an unexpected error), so that the thread does not end with the
// director's words alone. What was done before the failure is told too. The error is still returned
// to the caller. A failure to record the answer is only logged.
func (s *DirectorService) reportCommandFailure(ctx context.Context, projectID string, done outcome, cause error) {
	reason := commandFailureReason(cause)
	lines := append(slices.Clone(done.lines), "指示を実行できませんでした: "+reason)
	msg := assignIDs([]domain.ChatMessage{assistantText(strings.Join(lines, "\n"))})
	// The request may be the very thing that ended (a closed client): the notice is recorded anyway.
	if err := s.commitMessages(context.WithoutCancel(ctx), projectID, msg); err != nil {
		s.logger.Error("post director command failure notice failed", "project_id", projectID, "err", err)
	}
}

// commandFailureReason is the one line the thread gives for why a direction (or the reading of it)
// failed: the same classification the HTTP layer gives the status of. The errors of the service
// layer (a feature that is not set up, invalid input, a state that changed) are told here; the
// failures of the generation pipeline and the models are told by generation.FailureReason, and
// what is neither is an internal error (logged by whoever returns it).
func commandFailureReason(err error) string {
	var (
		ai       *AIError
		invalid  *ValidationError
		conflict *ConflictError
	)
	switch {
	case errors.As(err, &ai):
		return ai.Reason
	case errors.Is(err, ErrLocked):
		return "LOCK されているパラメータを変える指示のため実行しませんでした"
	case errors.As(err, &invalid):
		return invalid.Message
	case errors.Is(err, ErrUnavailable):
		return "この操作に必要な機能が有効になっていません（API キーと設定を確認してください）"
	case errors.As(err, &conflict):
		return conflict.Message
	case errors.Is(err, ErrNotFound):
		return "対象が見つかりません"
	case errors.Is(err, ErrBusy):
		return "この Shot はすでに生成中です"
	case errors.Is(err, ErrStageBusy):
		return "別の編集処理が実行中です。終わってからやり直してください"
	case errors.Is(err, ErrJudgeBusy):
		return "この Take はすでに Judge 中です"
	case errors.Is(err, ErrNotJudged):
		return "この Take はまだ Judge されていません。Judge が終わってから指示してください"
	case errors.Is(err, ErrAccepted):
		return "この Take は Judge に合格しているため Repair は不要です"
	case errors.Is(err, ErrConflict):
		return "状態が変わっています。画面を更新してください"
	}
	return generation.FailureReason(err) // includes the budget and the Brand violations
}

func (s *DirectorService) commitMessages(ctx context.Context, projectID string, msgs []domain.ChatMessage) error {
	if err := s.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: msgs}); err != nil {
		return notFound(err)
	}
	s.publishCreated(projectID, msgs)
	return nil
}
