package modelcatalog

import (
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/upscale"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
)

// The modes of an upscale workflow (Upscaler.Mode; upscale.Mode).
const (
	ModeModel   = string(upscale.Model)
	ModeTwoPass = string(upscale.TwoPass)
)

// Upscaler is an upscale workflow the generation test's "make it larger" can run (a workflow of an installed upscale model of the model
// manager, localcfg.Workflow.Upscale). It is not one of the Models: it takes no prompt of its own.
type Upscaler struct {
	ID    string `json:"id" jsonschema:"アップスケールのモデル識別子（comfyui:bundled-comfyui/<ワークフロー名>）。upscale_generation_result の model に渡す"`
	Label string `json:"label"`
	// Mode is how it enlarges: "model" (an upscale model only) or "two_pass" (the upscale model, then a low-denoise sampling).
	Mode string `json:"mode" jsonschema:"model（アップスケール用モデルだけ）/ two_pass（モデルで大きくしてから、低い denoise でもう一度サンプリングして仕上げる。元の結果のプロンプトとシードを引き継ぐ）"`
	// Native is the factor of the upscale model: a 2x or 4x request is made from it.
	Native int `json:"native" jsonschema:"アップスケール用モデルの倍率（4 なら 2 倍・4 倍の依頼はその倍率に合わせて縮める）"`
	// TakesPrompt: the workflow samples with the prompt of the source result.
	TakesPrompt bool `json:"takes_prompt"`
	// MaxOutputPixels is the most pixels (width × height) the workflow makes (0: only the limit of the generation test).
	MaxOutputPixels int  `json:"max_output_pixels,omitempty"`
	Available       bool `json:"available"`
	// Unavailable is why it cannot be run now (domain.ExcludedNoBackend, domain.ExcludedCircuitOpen, the age gate).
	Unavailable        string            `json:"unavailable,omitempty"`
	UnavailableMessage string            `json:"unavailable_message,omitempty"`
	License            modellicense.Info `json:"license" jsonschema:"商用利用の可否（commercial: yes / conditional / no / unknown）・ライセンス名・条件・出典 URL・確認日。確認できていないモデルは unknown"`
}

// Upscalers lists the upscale workflows of the running setting in key order (an upscale model that is not installed has none), the ones
// the age gate hides left out.
func (c Catalog) Upscalers() []Upscaler {
	cfg := c.Adapter.Config()
	var out []Upscaler
	for _, key := range slices.Sorted(maps.Keys(cfg.LocalModels)) {
		endpoint, name, ok := localcfg.SplitKey(key)
		ep, registered := cfg.Endpoints[endpoint]
		m := cfg.LocalModels[key]
		// Only a ComfyUI server of a registered endpoint runs a workflow.
		if !ok || !registered || ep.Kind != localcfg.KindComfyUI || !m.IsUpscale() {
			continue
		}
		if id := localcfg.ID(ep.Kind, endpoint, name); !c.hidden(domain.ModelName(id)) {
			out = append(out, c.upscaler(id, *m.Workflow.Upscale))
		}
	}
	return out
}

// Upscaler returns the upscale workflow with the identifier; false when there is none (or it is hidden).
func (c Catalog) Upscaler(id string) (Upscaler, bool) {
	for _, u := range c.Upscalers() {
		if u.ID == id {
			return u, true
		}
	}
	return Upscaler{}, false
}

func (c Catalog) upscaler(id string, u upscale.Spec) Upscaler {
	m := domain.ModelName(id)
	out := Upscaler{
		ID: id, Label: u.Label, Mode: string(u.Mode), Native: u.Native, TakesPrompt: u.Mode == upscale.TwoPass,
		MaxOutputPixels: u.MaxOutputPixels, License: modellicense.Of(id),
	}
	if reason := c.Adapter.Unavailable(m); reason != "" {
		out.Unavailable, out.UnavailableMessage = reason, unavailableMessage(reason, SourceLocal)
	} else {
		out.Available = true
	}
	return out
}
