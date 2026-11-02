package modelmgr

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
)

func registerCommercial(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "get_commercial_only",
		"設定「商用OKのモデルだけを使う」の状態。オンの間、画面のモデル一覧は「商用OKのみ」で始まり、list_generation_models と list_model_catalog は commercial 引数を省くと商用OK（license.commercial=yes）のモデルだけを返す。生成そのものは止めない / The setting that makes the model lists start filtered to commercially usable models.",
		h.commercialOnly)
	mcpkit.Write(r, "set_commercial_only",
		"設定「商用OKのモデルだけを使う」を切り替える。オンにすると、モデルの一覧（画面・MCP）は既定で商用OKのモデルだけになる（commercial 引数や画面のフィルターで広げられる）。ユーザーが求めたときだけ呼ぶ / Turn the setting on or off.",
		h.setCommercialOnly, mcpkit.Idempotent())
}

// CommercialOnlyOutput is the setting.
type CommercialOnlyOutput struct {
	CommercialOnly bool `json:"commercial_only" jsonschema:"true: モデルの一覧は既定で商用OKのモデルだけ"`
}

func (h handlers) commercialOnly(_ context.Context, _ Input) (CommercialOnlyOutput, error) {
	m, err := h.mm()
	if err != nil {
		return CommercialOnlyOutput{}, err
	}
	return CommercialOnlyOutput{CommercialOnly: m.CommercialOnly()}, nil
}

// CommercialOnlyInput is the setting to apply.
type CommercialOnlyInput struct {
	Enabled bool `json:"enabled" jsonschema:"true: モデルの一覧を既定で商用OKだけにする / false: すべて表示する"`
}

func (h handlers) setCommercialOnly(_ context.Context, in CommercialOnlyInput) (CommercialOnlyOutput, error) {
	m, err := h.mm()
	if err != nil {
		return CommercialOnlyOutput{}, err
	}
	if err := m.SetCommercialOnly(in.Enabled); err != nil {
		return CommercialOnlyOutput{}, err
	}
	return CommercialOnlyOutput{CommercialOnly: m.CommercialOnly()}, nil
}
