// Package settings holds the MCP tools of the global settings screen (GET /settings): the LLM assignment,
// the image / video model assignment and the update check. API keys are in tools/credentials. The MCP
// server section (bind address, bearer tokens) has no tool by design: an agent never manages its own access.
//
// The sections show what is saved in the settings files: the LLM assignment applies from the next start of
// the app, the image / video model assignment at once.
package settings

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package. The services of the sections come from r.Settings; a tool whose
// section the app does not have answers "unavailable".
func Register(r *mcpkit.Registrar, _ *service.Services) {
	h := handlers{r: r}
	mcpkit.Read(r, "get_global_settings",
		"全体設定画面の内容（LLM の割り当て・画像／動画モデルの割り当て・バージョンとアップデート）。API キーは list_credentials。保存済みの設定で、LLM の割り当ては次の起動から、画像・動画モデルはすぐに反映 / The global settings screen without secrets.",
		h.overview)
	mcpkit.Write(r, "set_llm_assignment",
		"Agent ごとの LLM（主・代替）の割り当てを保存する。指定した Agent だけ変わる。反映にはアプリの再起動が必要 / Save the LLM routes of some Agents (effective after a restart).",
		h.setLLM, mcpkit.Idempotent())
	mcpkit.Destroy(r, "reset_llm_assignment",
		"LLM の割り当てを既定に戻す（保存した上書きをすべて消す）。反映にはアプリの再起動が必要 / Drop every saved LLM route override.",
		h.resetLLM, mcpkit.Idempotent())
	mcpkit.Write(r, "set_model_assignment",
		"画像・動画モデルの割り当てを保存する。指定した用途・Quality Policy だけ変わる。model には組み込みの名前のほか、ローカル・代替モデルの識別子（comfyui:<接続先>/<名前>）も使える。すぐに反映される / Save image / video model routes, including local models (effective at once).",
		h.setModels, mcpkit.Idempotent())
	mcpkit.Destroy(r, "reset_model_assignment",
		"画像・動画モデルの割り当てを既定に戻す。すぐに反映される / Drop the saved image / video model overrides (effective at once).",
		h.resetModels, mcpkit.Idempotent())
	mcpkit.Read(r, "get_audio_providers",
		"音声・口パクのプロバイダーの設定。声（elevenlabs か local＝GPT-SoVITS）・口パク（synclabs か local＝MuseTalk 1.5）・ローカルが使えないときにクラウドへ切り替えるか（既定はオフ）と、ローカルの状態・使えない理由（macOS の非対応の理由を含む）・クラウドの API キーの有無（値は返さない）。保存した内容は次の音声作成から使われる / The voice and lip-sync providers and whether the local ones can be used.",
		h.getAudio)
	mcpkit.Write(r, "set_audio_providers",
		"声・口パクのプロバイダーと「ローカルが使えないときはクラウドに切り替える」を保存する。指定した項目だけ変わる。この PC で動かせないとき（macOS など）に local を選ぶと validation で、理由を返す。fallback_to_cloud をオンにすると、ローカルが使えない実行がクラウドで作られ、API 費用が出る。次の音声作成から使われる / Save the voice and lip-sync providers and the opt-in fallback to the cloud.",
		h.setAudio, mcpkit.Idempotent())
	mcpkit.Read(r, "get_update_status",
		"アップデートの状態（今のバージョン・最後の確認の結果・新しいバージョン）。アップデートできない版（ヘッドレス）では unavailable / The state of the update check.",
		h.updateStatus)
	mcpkit.Write(r, "check_for_update",
		"今すぐアップデートを確認する。新しいリリースを検証できると、画面と同じく、アプリが終了してインストーラーが走る（生成などの作業中は待つ）/ Check for a newer release now; a verified one makes the app quit and install it.",
		h.checkUpdate, mcpkit.OpenWorld())
}

type handlers struct{ r *mcpkit.Registrar }

// Input has no field.
type Input struct{}

// pinnedMessage is what the screen says for a file the operator fixed with a start-up option.
const pinnedMessage = "起動オプションで固定されているため、この画面では変更できません"

func unavailable(what string) error {
	return &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: what + " はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
}

func invalidInput(field, message string) error {
	return &service.ValidationError{Field: field, Message: message}
}

// Overview is the global settings screen without secrets.
type Overview struct {
	LLM    LLMSection    `json:"llm"`
	Models ModelsSection `json:"models"`
	Update UpdateSection `json:"update"`
}

func (h handlers) overview(_ context.Context, _ Input) (Overview, error) {
	s := h.r.Settings
	out := Overview{LLM: LLMSection{Available: s.LLM != nil}, Models: ModelsSection{Available: s.Models != nil}}
	if s.LLM != nil {
		out.LLM = llmSection(s.LLM)
	}
	if s.Models != nil {
		out.Models = modelsSection(s.Models)
	}
	out.Update = updateSection(h.r)
	return out, nil
}

// mapSaveError turns the refusals of the settings services into the errors the tools map: an invalid input
// is a validation error, a pinned file a conflict. Anything else (a file that cannot be written) is internal.
func mapSaveError(err error, invalid func(error) (string, bool), pinned error) error {
	if msg, ok := invalid(err); ok {
		return invalidInput("", msg)
	}
	if errors.Is(err, pinned) {
		return &service.ConflictError{Message: pinnedMessage}
	}
	return err
}
