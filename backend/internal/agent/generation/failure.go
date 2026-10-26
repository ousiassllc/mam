package generation

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// fail tells the user why the job stopped and returns the error for the job record.
func (o *Orchestrator) fail(ctx context.Context, ref repository.ShotRef, a Action, err error) error {
	o.release(ctx, ref.Shot.ID)
	if ctx.Err() != nil || errors.Is(err, repository.ErrNotFound) {
		return err // shutting down (nobody is told) or the Shot is gone
	}
	o.logger.Error("generation failed", "shot_id", ref.Shot.ID, "action", a, "err", err)
	reason := FailureReason(err)
	what := "生成"
	if a == ActionReferencePack {
		what = "Reference Pack の作成"
	}
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Shot %d の%sに失敗しました: %s\n対処したら、もう一度実行してください。", ref.Shot.OrderIndex+1, what, reason),
	}
	if aerr := o.chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, notice); aerr != nil {
		o.logger.Error("post failure notice failed", "shot_id", ref.Shot.ID, "err", aerr)
	}
	o.notify(Progress{ShotID: ref.Shot.ID, Action: a, Stage: StageFailed, Message: reason})
	return errors.New(reason)
}

// FailureReason is the user-facing cause of a failure. Details stay in the log.
func FailureReason(err error) string {
	switch {
	case errors.Is(err, modeladapter.ErrNoAPIKey), errors.Is(err, llm.ErrNoAPIKey):
		return "生成モデルまたは LLM の API キーが設定されていません（設定画面でキーを登録してください）"
	case errors.Is(err, agegate.ErrRefused):
		return agegate.Reason(err)
	case errors.Is(err, ErrBudgetExceeded):
		return "予算上限を超えるため生成できません"
	case errors.Is(err, domain.ErrBrandViolation):
		msg := "Brand / Safety Constraint に違反するため生成できません"
		if bv := new(domain.BrandViolationError); errors.As(err, &bv) {
			msg += "（" + bv.Message() + "）"
		}
		return msg
	case errors.Is(err, ErrJudgeUnavailable):
		return "AI Video Judge が利用できないため、候補から最良の Take を選べません"
	case errors.Is(err, ErrGenerationTimeLimit):
		return "生成が制限時間（Max generation time）を超えました。設定画面で制限時間を延ばすか、ポリシーを見直してください"
	case modeladapter.RateLimited(err):
		return "生成モデルのレート制限に達しました。しばらく待ってから再実行してください"
	case errors.Is(err, modeladapter.ErrCircuitOpen):
		return "生成モデルが一時的に利用できません。しばらく待ってから再実行してください"
	case errors.Is(err, llm.ErrCircuitOpen):
		return "AI サービスが一時的に利用できません。しばらく待ってから再実行してください"
	case errors.Is(err, modeladapter.ErrRejected):
		return "生成モデルがリクエストを拒否しました（コンテンツポリシー等）。指示を変えて再実行してください"
	case errors.Is(err, modeladapter.ErrTimeout), errors.Is(err, llm.ErrTimeout):
		return "生成モデルの応答がタイムアウトしました"
	case errors.Is(err, media.ErrTimeout):
		return "動画・音声の処理（ffmpeg）が制限時間を超えました。素材ファイルを確認して再実行してください"
	case errors.Is(err, llm.ErrInvalidOutput), errors.Is(err, errInvalidPlan):
		return "AI の出力を制作に使える形で受け取れませんでした"
	}
	var pe *modeladapter.ProviderError
	if errors.As(err, &pe) {
		return "生成モデルの呼び出しに失敗しました"
	}
	var api *llm.APIError
	if errors.As(err, &api) {
		if api.Status == http.StatusTooManyRequests {
			return "AI サービスのレート制限に達しました。しばらく待ってから再実行してください"
		}
		return "AI サービスの呼び出しに失敗しました"
	}
	return "内部エラーが発生しました"
}
