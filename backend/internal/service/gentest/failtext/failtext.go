// Package failtext words the failures of the generation test that depend on where the call went (a local endpoint, the LLM that translates).
package failtext

import (
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
)

// LocalTimeout says what happened to a call to an endpoint the user registered that ran past its limit, and what to do
// next: a ComfyUI job that showed no progress for the endpoint's timeout, a call that ran past the ceiling a ComfyUI call
// has (the timeout cannot be raised above it), or a call that ran past the endpoint's timeout. It says nothing about
// billing: a local call costs nothing per call, and mam cancels the job on the server. ok is false for any other error.
func LocalTimeout(err error) (msg string, ok bool) {
	var te *localcfg.TimeoutError
	if !errors.As(err, &te) {
		return "", false
	}
	switch {
	case te.Stalled:
		return "ローカルの生成が" + duration(te.After) + "のあいだ進まなかったため打ち切りました。ComfyUI が止まっていないか確かめてください。重いモデルで進みの出ない処理が続く場合は、設定画面の接続先でタイムアウト（進みが止まってから打ち切るまでの時間）を延ばしてください", true
	case te.After >= localcfg.MaxTimeoutSeconds*time.Second:
		return "ローカルの生成が全体の上限（" + duration(te.After) + "）を超えたため打ち切りました。解像度や秒数を小さくして実行し直してください", true
	}
	return "ローカルの生成が接続先の待ち時間（" + duration(te.After) + "）を超えたため打ち切りました。設定画面の接続先でタイムアウトを延ばすか、解像度や秒数を小さくして実行し直してください", true
}

// duration is a time limit in words: whole hours, else whole minutes, else seconds.
func duration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%d時間", d/time.Hour)
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%d分", d/time.Minute)
	}
	return fmt.Sprintf("%d秒", int(d.Round(time.Second)/time.Second))
}

// Translate is what the user is told when the prompts could not be translated.
func Translate(err error) string {
	const tail = "。何も生成していません。もう一度試すか、英訳を OFF にしてください"
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		return "プロンプトを英語に訳せませんでした: LLM の API キーが登録されていません（設定の「LLM の割り当て」の Prompt Planner が使うプロバイダー）" + tail
	case errors.Is(err, llm.ErrCircuitOpen):
		return "プロンプトを英語に訳せませんでした: LLM が一時的に使えません" + tail
	case errors.Is(err, llm.ErrTimeout):
		return "プロンプトを英語に訳せませんでした: LLM の応答が時間内に返りませんでした" + tail
	case errors.Is(err, prompttranslate.ErrNotTranslated), errors.Is(err, llm.ErrInvalidOutput):
		return "プロンプトを英語に訳せませんでした: LLM の答えが翻訳になっていません" + tail
	}
	return "プロンプトを英語に訳せませんでした: LLM の呼び出しに失敗しました" + tail
}
