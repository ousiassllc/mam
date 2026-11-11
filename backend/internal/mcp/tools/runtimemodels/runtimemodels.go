// Package runtimemodels holds the MCP tools of the models of a local runtime that has several (list_local_runtime_models,
// select_local_runtime_model; Irodori-TTS: v4.1-Small, v4-Large, ...; spec/api/mcp-tools-model-manager.md, spec/architecture/model-manager/local-runtime.md §14).
// It is a package of its own because the modelmgr package is at its size limit. The tools call the same runtimechoices.Service as the screen, so the same
// rules hold: a model whose license has to be accepted (the Gemma Terms of the Large models) is chosen only with the version of its text that the
// user was shown, which is the user's answer to pass on; the tool never makes it up, and nothing in mam passes it by itself.
package runtimemodels

import (
	"cmp"
	"context"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices"
)

// Register adds the tools. They answer "unavailable" when the app has no runtime manager (svc.LocalRuntimes is nil).
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc}
	mcpkit.Read(r, "list_local_runtime_models",
		"ローカルの実行環境で選べるモデルの一覧（Irodori-TTS: v4.1-Small・v4.1-Small-MF・v4-Large・v4-Large の int8 / int4 / fp8。llama-cpp: Gemma 4 12B・Qwen3.8-27B・DeepSeek-R1-Distill-Qwen-14B の純正と非検閲〔uncensored: true。成人向け相当〕）。各モデルの種類（kind: tts・llm）・言語（languages）・ライセンスと商用利用の可否・倫理上の制限（cautions）・必要なビデオメモリ（この PC の GPU との判定つき）・取得するサイズ・使用中か・同意が要るか（consent_needed。同意の本文と版は consent）を返す。モデルを持たない実行環境は空。runtime_id を省くとモデルを持つすべての実行環境のモデルを返し、kinds（audio=声・llm）・languages（ja・en）・uncensored・vram_fit・commercial・q で絞れる（例: 日本語の声は kinds=[audio]・languages=[ja]） / The models of a local runtime with their license, restrictions, VRAM and whether the license has to be accepted.",
		h.list)
	mcpkit.Write(r, "select_local_runtime_model",
		"ローカルの実行環境が使うモデルを選ぶ（動いているサーバーは止める。重みは、モデルを選んだあとに install_local_runtime_weights で取得する）。ライセンスの同意が要るモデル（Gemma Terms の v4-Large 系。list_local_runtime_models の consent_needed）は、ユーザーに consent.text を見せ、ユーザーが同意した後にだけ accept_license_version に consent.version を渡す。エージェントが代わりに渡してはならない（渡さなければ conflict で拒否され、何も記録しない）/ Choose the model of a runtime; a model with a license to accept needs accept_license_version, which only the user's own agreement may supply.",
		h.selectModel, mcpkit.Idempotent())
}

type handlers struct{ svc *service.Services }

func (h handlers) choices() (runtimechoices.Service, error) {
	if h.svc.LocalRuntimes == nil {
		return runtimechoices.Service{}, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "ローカルの実行環境の管理はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return runtimechoices.Service{Runtimes: h.svc.LocalRuntimes}, nil
}

// mapError is the same mapping as the other tools of the runtimes: the screen's kinds (validation, conflict, not_found).
func mapError(err error) error {
	kind, msg := mmview.RefuseRuntime(err)
	switch kind {
	case mmview.RefusalInvalid:
		return &service.ValidationError{Message: msg}
	case mmview.RefusalConflict:
		return &service.ConflictError{Message: msg}
	case mmview.RefusalNotFound:
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: msg, Status: http.StatusNotFound}
	}
	return err
}

// RuntimeInput names a runtime.
type RuntimeInput struct {
	RuntimeID string `json:"runtime_id" jsonschema:"list_local_runtimes の id（irodori-tts など）"`
}

// ListInput names a runtime (or, left out, every runtime that has models) and narrows its models by the conditions every model picker has
// (modelfilter.Condition, #1011): kinds (audio: tts の声 / llm), languages (ja・en), uncensored, vram_fit, commercial, speeds, qualities and q. A runtime with models
// has models of one kind, so kinds and languages tell the models of different runtimes apart when runtime_id is left out.
type ListInput struct {
	RuntimeID string `json:"runtime_id,omitempty" jsonschema:"list_local_runtimes の id（irodori-tts など）。省略すると、モデルを持つすべての実行環境のモデルを返す（kinds・languages で横断して絞れる）"`
	modelfilter.Condition
}

// ListOutput is the models of a runtime (of every runtime that has models when none was named; each model says its runtime_id).
type ListOutput struct {
	RuntimeID        string                `json:"runtime_id,omitempty"`
	Models           []runtimechoices.View `json:"models"`
	CommercialFilter string                `json:"commercial_filter,omitempty" jsonschema:"適用した商用利用の絞り込み。commercial を省くと設定「商用OKのモデルだけを使う」に従う"`
}

func (h handlers) list(ctx context.Context, in ListInput) (ListOutput, error) {
	s, err := h.choices()
	if err != nil {
		return ListOutput{}, err
	}
	cond := in.Normalize()
	if err := cond.Validate(); err != nil {
		// the value that is no word of the condition is told in English by the filter: it is the detail of the common error (#1010), the sentence is Japanese
		return ListOutput{}, apperr.New(apperr.CodeInvalidInput).WithCause("絞り込みの条件に、選べない値があります。").Wrap(err)
	}
	if cond.Commercial == "" { // without commercial the setting "商用OKのモデルだけを使う" decides, as list_model_catalog does
		cond.Commercial = string(modellicense.DefaultFilter())
		cond = cond.Normalize()
	}
	out := ListOutput{RuntimeID: in.RuntimeID, Models: []runtimechoices.View{}, CommercialFilter: cmp.Or(cond.Commercial, string(modellicense.FilterAll))}
	ids := []string{in.RuntimeID}
	if in.RuntimeID == "" {
		ids = ids[:0]
		for _, v := range h.svc.LocalRuntimes.List(ctx) {
			ids = append(ids, v.ID)
		}
	}
	for _, id := range ids {
		views, err := s.Views(ctx, id)
		if err != nil {
			return ListOutput{}, mapError(err)
		}
		out.Models = append(out.Models, runtimechoices.Where(views, cond)...)
	}
	return out, nil
}

// SelectInput names the model to use.
type SelectInput struct {
	RuntimeID            string `json:"runtime_id" jsonschema:"list_local_runtimes の id（irodori-tts など）"`
	ModelID              string `json:"model_id" jsonschema:"list_local_runtime_models の id（v4.1-small など）"`
	AcceptLicenseVersion string `json:"accept_license_version,omitempty" jsonschema:"ユーザーが consent.text を見て同意したときだけ、その consent.version を渡す。同意が要らないモデルでは不要。エージェントが代わりに渡してはならない"`
}

func (h handlers) selectModel(ctx context.Context, in SelectInput) (ListOutput, error) {
	s, err := h.choices()
	if err != nil {
		return ListOutput{}, err
	}
	var accept *runtimechoices.Accept
	if in.AcceptLicenseVersion != "" {
		accept = &runtimechoices.Accept{Version: in.AcceptLicenseVersion}
	}
	if err := s.Select(ctx, in.RuntimeID, in.ModelID, accept); err != nil {
		return ListOutput{}, mapError(err)
	}
	return h.list(ctx, ListInput{RuntimeID: in.RuntimeID})
}
