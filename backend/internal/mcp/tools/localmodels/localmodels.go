// Package localmodels holds the MCP tools of the "ローカル・代替モデル" section of the settings screen: the
// ComfyUI and OpenAI-compatible endpoints the user registered, the models defined on them, the connection
// test and the API key of each endpoint (spec/api/mcp-tools-local-models.md, spec/architecture/local-models.md).
//
// A saved endpoint or model applies at once (localmodels.Service reloads the Model Adapter). The assignment
// of a model to an image purpose or a Quality Policy is set_model_assignment (tools/settings): the models
// defined here are choices there under their identifier "<kind>:<endpoint>/<name>". Generation itself is
// generate_shot and the other generation tools (job_id, get_job). No tool returns an API key: an endpoint
// shows whether it has one and the last characters of it.
package localmodels

import (
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

// Register adds the tools of this package. They answer "unavailable" when the app has no local models
// service (svc.LocalModels is nil).
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc}
	registerEndpoints(r, h)
	registerModels(r, h)
}

type handlers struct{ svc *service.Services }

// local is the service, or the "unavailable" error.
func (h handlers) local() (*localmodels.Service, error) {
	if h.svc.LocalModels == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "ローカル・代替モデルの設定はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.LocalModels, nil
}

// pinnedMessage is what the screen says for a file the operator fixed with --model-config.
const pinnedMessage = "起動オプション（--model-config）で固定されているため、接続先とモデルは変更できません（API キーと接続テストは使えます）"

// Input has no field.
type Input struct{}

// mapError turns the refusals of the service into the errors the tools map: ErrPinned and ErrExists are
// conflicts, ErrNotFound is not found and an invalid input is a validation error with its message.
func mapError(err error) error {
	var invalid *localmodels.InvalidError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, localmodels.ErrPinned):
		return &service.ConflictError{Message: pinnedMessage}
	case errors.Is(err, localmodels.ErrExists):
		return &service.ConflictError{Message: "同じ名前の接続先またはモデルがすでにあります（名前は後から変えられません。削除して作り直してください）"}
	case errors.Is(err, localmodels.ErrNotFound):
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: "その接続先またはモデルは見つかりません（list_local_endpoints で確かめてください）", Status: http.StatusNotFound}
	case errors.As(err, &invalid):
		return &service.ValidationError{Message: invalid.Message}
	}
	return err
}
