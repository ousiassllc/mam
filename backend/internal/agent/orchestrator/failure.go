package orchestrator

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
)

// failureReason is the user-facing cause of a failure. Details stay in the log.
func failureReason(err error) string {
	var api *llm.APIError
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		return "LLM の API キーが設定されていません（設定画面で Anthropic / OpenAI / Google のキーを登録してください）"
	case errors.Is(err, planning.ErrInvalidPlan), errors.Is(err, llm.ErrInvalidOutput):
		return "AI の出力を制作に使える形で受け取れませんでした"
	case errors.As(err, &api) && api.Notice != "":
		return api.Notice // the cause the provider gave: a ChatGPT usage limit, a sign-in to repeat
	case errors.Is(err, llm.ErrTimeout):
		return "AI の応答がタイムアウトしました"
	case errors.Is(err, llm.ErrCircuitOpen):
		return "AI サービスが一時的に利用できません。しばらく待ってから再実行してください"
	case errors.As(err, &api) && api.Status == 429:
		return "AI サービスのレート制限に達しました"
	case errors.As(err, &api):
		return "AI サービスの呼び出しに失敗しました"
	}
	return "内部エラーが発生しました"
}
