package modelcatalog

import (
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/videoenhance"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// VideoEnhancer is a model that makes a video larger or smoother (Issue #1031): a local workflow (SeedVR2, RIFE: localcfg.Workflow.VideoEnhance)
// or a model of a paid cloud (Topaz). It is not one of the Models: it takes no prompt, only a video.
type VideoEnhancer struct {
	ID          string `json:"id" jsonschema:"モデル識別子。estimate_generation_video_enhance / enhance_generation_video の model に渡す（ローカルは comfyui:bundled-comfyui/<ワークフロー名>、Topaz は topaz:<モデル名>）"`
	Op          string `json:"op" jsonschema:"upscale（大きくする）/ interpolate（コマを増やして滑らかにする）"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Provider    string `json:"provider" jsonschema:"local（この PC の ComfyUI。無料）/ topaz（Topaz Video API。有料クラウド）"`
	// Paid is true for a model of a paid cloud: it needs an estimate and the confirmation of its credits before it runs.
	Paid bool `json:"paid" jsonschema:"true は有料クラウド（estimate_generation_video_enhance で見積りを取り、estimate_id と confirmed_credits を渡さないと実行できない）。false はローカルで無料・確認不要"`
	// MaxOutputPixels is the most pixels (width × height) of one frame the model makes (0: only the limit of the generation test).
	MaxOutputPixels int  `json:"max_output_pixels,omitempty"`
	Available       bool `json:"available"`
	// Unavailable is why it cannot be run now (domain.ExcludedNoBackend, domain.ExcludedCircuitOpen, the age gate); "no_key" for a cloud model without its key.
	Unavailable        string            `json:"unavailable,omitempty"`
	UnavailableMessage string            `json:"unavailable_message,omitempty"`
	License            modellicense.Info `json:"license" jsonschema:"商用利用の可否（commercial: yes / conditional / no / unknown）・ライセンス名・条件・出典 URL・確認日。確認できていないモデルは unknown"`
	// Attribution is the credit that must be shown wherever the model is offered or used ("Powered by Topaz Labs"); empty for a model without one.
	Attribution string `json:"attribution,omitempty" jsonschema:"表示が必要な出典（Topaz は Powered by Topaz Labs）"`
}

// UnavailableNoKey is VideoEnhancer.Unavailable of a cloud model whose API key is not registered.
const UnavailableNoKey = "no_key"

// VideoEnhancers lists the local video enhancement workflows of the running setting in key order, the ones the age gate hides left out.
func (c Catalog) VideoEnhancers() []VideoEnhancer {
	cfg := c.Adapter.Config()
	var out []VideoEnhancer
	for _, key := range slices.Sorted(maps.Keys(cfg.LocalModels)) {
		endpoint, name, ok := localcfg.SplitKey(key)
		ep, registered := cfg.Endpoints[endpoint]
		m := cfg.LocalModels[key]
		// Only a ComfyUI server of a registered endpoint runs a workflow.
		if !ok || !registered || ep.Kind != localcfg.KindComfyUI || !m.IsVideoEnhance() {
			continue
		}
		if id := localcfg.ID(ep.Kind, endpoint, name); !c.hidden(domain.ModelName(id)) {
			out = append(out, c.videoEnhancer(id, *m.Workflow.VideoEnhance))
		}
	}
	return out
}

func (c Catalog) videoEnhancer(id string, v videoenhance.Spec) VideoEnhancer {
	out := VideoEnhancer{
		ID: id, Op: string(v.Op), Label: v.Label, Provider: "local", MaxOutputPixels: v.MaxOutputPixels, License: modellicense.Of(id),
	}
	if reason := c.Adapter.Unavailable(domain.ModelName(id)); reason != "" {
		out.Unavailable, out.UnavailableMessage = reason, unavailableMessage(reason, SourceLocal)
	} else {
		out.Available = true
	}
	return out
}

// Item is the video enhancement model as the filter sees it: the kind of its operation (larger or smoother), local (on this PC) or api (Topaz, paid), installed.
func (v VideoEnhancer) Item() modelfilter.Item {
	kind, origin, tag := modelfilter.KindVideoUpscale, modelfilter.OriginLocal, "video_upscale"
	if v.Op == "interpolate" {
		kind, tag = modelfilter.KindVideoInterpolate, "video_interpolate"
	}
	if v.Paid {
		origin = modelfilter.OriginAPI
	}
	return modelfilter.Item{
		ID: v.ID, Name: v.Label, Kinds: []modelfilter.Kind{kind}, Commercial: v.License.Commercial, Installed: true, Origin: origin, Provider: v.Provider,
		Tags: []string{tag, v.Op}, Available: v.Available, Reason: v.UnavailableMessage,
	}
}

// Availability is whether the model can be chosen now, in the shape every picker shares: a cloud model without its key is "no_backend" with the
// settings screen of the API keys as its route.
func (v VideoEnhancer) Availability() modelfilter.Availability {
	if v.Available {
		return modelfilter.Usable()
	}
	source, code := SourceLocal, modelfilter.Reason(v.Unavailable)
	switch v.Unavailable {
	case UnavailableNoKey:
		source, code = SourceBuiltin, modelfilter.ReasonNoKey
	case agegate.Excluded:
		code = modelfilter.ReasonAgeGate
	}
	return modelfilter.Unusable(code, v.UnavailableMessage, modelfilter.RouteOf(code, source))
}
