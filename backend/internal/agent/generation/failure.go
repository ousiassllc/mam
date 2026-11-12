package generation

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/generation/failburst"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// fail tells the user why the job stopped and returns the error for the job record.
func (o *Orchestrator) fail(ctx context.Context, ref repository.ShotRef, a Action, err error) error {
	o.release(ctx, ref.Shot.ID)
	if ctx.Err() != nil || errors.Is(err, repository.ErrNotFound) {
		return err // shutting down (nobody is told) or the Shot is gone
	}
	failure := Failure(err)
	attrs := []any{"shot_id", ref.Shot.ID, "action", a, "err", err}
	if failure.Code == apperr.CodeUnknown {
		failure.ID = apperr.NewID() // the notice says it, the log line has it: a person who reports the failure and the log meet
		attrs = append(attrs, "error_id", failure.ID)
	}
	o.logger.Error("generation failed", attrs...)
	reason := failure.View(apperr.LangJa).Sentence(apperr.LangJa)
	if failure.ID != "" {
		reason += "（" + apperr.Text(apperr.LangJa, "ui.error_id", nil) + ": " + failure.ID + "）"
	}
	what := "生成"
	if a == ActionReferencePack {
		what = "Reference Pack の作成"
	}
	shot := fmt.Sprintf("Shot %d の%s", ref.Shot.OrderIndex+1, what) // a failure with an ID of its own is one of its kind; any other is told once per burst of jobs (failburst)
	if failure.ID != "" || o.burst.First(ref.ProjectID, reason, shot) {
		notice := failmsg.Notice(err, fmt.Sprintf("%sに失敗しました: %s\n対処したら、もう一度実行してください。", shot, reason))
		if aerr := o.chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, notice); aerr != nil {
			o.logger.Error("post failure notice failed", "shot_id", ref.Shot.ID, "err", aerr)
		}
	}
	o.notify(Progress{ShotID: ref.Shot.ID, Action: a, Stage: StageFailed, Message: reason})
	return apperr.WithText(reason, failure)
}

// FailureReason is the user-facing cause of a failure and what to do: the cause and the remedy of Failure in Japanese. Details stay in
// the log (the reason never carries what the vendor said: a path or a response can be in it).
func FailureReason(err error) string {
	return Failure(err).View(apperr.LangJa).Sentence(apperr.LangJa)
}

// Failure is err as the common error (#1010): what the generation pipeline refuses itself (the budget, a Brand violation, the age gate, the
// time limit, an AI that gave nothing usable) has its own sentence as the cause; the failures of the models, the LLM, ffmpeg and the network
// are errmap's. The catalog adds the remedy.
func Failure(err error) *apperr.Error {
	own := func(code apperr.Code, cause string) *apperr.Error { return apperr.New(code).WithCause(cause).Wrap(err) }
	switch {
	case errors.Is(err, agegate.ErrRefused):
		return own(apperr.CodeForbidden, agegate.Reason(err))
	case errors.Is(err, modelroute.ErrNoUsableModel): // before the job is queued, or in the job when the models were gone: the cause names the models and the candidates
		return own(apperr.CodeNoUsableModel, modelroute.CauseOf(err))
	case errors.Is(err, ErrBudgetExceeded):
		return own(apperr.CodeBudget, "予算上限を超えるため生成できません。")
	case errors.Is(err, domain.ErrBrandViolation):
		msg := "Brand / Safety Constraint に違反するため生成できません"
		if bv := new(domain.BrandViolationError); errors.As(err, &bv) {
			msg += "（" + bv.Message() + "）"
		}
		return own(apperr.CodeForbidden, msg+"。")
	case errors.Is(err, ErrJudgeUnavailable):
		return own(apperr.CodeUnavailable, "AI Video Judge が利用できないため、候補から最良の Take を選べません。")
	case errors.Is(err, ErrGenerationTimeLimit):
		return apperr.New(apperr.CodeGenTimeLimit).Wrap(err)
	case errors.Is(err, errInvalidPlan):
		return apperr.New(apperr.CodeLLMInvalidOutput).Wrap(err)
	}
	return errmap.Explain(err)
}

// endBurst is the end of a generate job of the Project. The last job of a burst tells once which other Shots failed for the same causes as the notices before (failburst).
func (o *Orchestrator) endBurst(ctx context.Context, projectID string) {
	for _, text := range failburst.Summaries(o.burst.Leave(projectID)) {
		if err := o.chat.Announce(context.WithoutCancel(ctx), projectID, domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: text}); err != nil {
			o.logger.Error("post failure summary failed", "project_id", projectID, "err", err)
		}
	}
}
