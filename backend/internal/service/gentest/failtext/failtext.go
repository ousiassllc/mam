// Package failtext words the failure of the generation test that is its own: the prompts that could not be translated. The failures of the calls to
// a model are the common error (package apperr, errmap).
package failtext

import (
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
)

// Translate is what the user is told when the prompts could not be translated.
func Translate(err error) string {
	const tail = "。何も生成していません。もう一度試すか、英訳を OFF にしてください"
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		return "プロンプトを英語に訳せませんでした: " + strings.TrimSuffix(failmsg.NoKeyMessage(err), "。") + tail
	case errors.Is(err, llm.ErrCircuitOpen):
		return "プロンプトを英語に訳せませんでした: LLM が一時的に使えません" + tail
	case errors.Is(err, llm.ErrTimeout):
		return "プロンプトを英語に訳せませんでした: LLM の応答が時間内に返りませんでした" + tail
	case errors.Is(err, prompttranslate.ErrNotTranslated), errors.Is(err, llm.ErrInvalidOutput):
		return "プロンプトを英語に訳せませんでした: LLM の答えが翻訳になっていません" + tail
	}
	return "プロンプトを英語に訳せませんでした: LLM の呼び出しに失敗しました" + tail
}
