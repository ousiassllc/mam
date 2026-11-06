// Package modelcatalog lists the models the generation test can use, with what each can do, what it costs and why one cannot be
// chosen now.
package modelcatalog

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/promptweight"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// Where a model comes from (ModelInfo.Source).
const (
	SourceBuiltin  = "builtin"  // a cloud model with a built-in adapter
	SourceChatGPT  = "chatgpt"  // the image model made with a ChatGPT subscription (#832)
	SourceLocal    = "local"    // a model defined on a registered ComfyUI / OpenAI-compatible endpoint (#831, #833)
	SourceWorkflow = "workflow" // a saved ComfyUI workflow (#834)
)

// How a cost is known (ModelInfo.Price.Basis, Estimate lines).
const (
	BasisPriced  = gentestrepo.CostPriced  // the price is known: USD
	BasisFree    = gentestrepo.CostFree    // a local model: 0 USD
	BasisUnknown = gentestrepo.CostUnknown // no price (a subscription, a cloud model without a price in models.json)
)

// VideoCaps are the capabilities of a video model.
type VideoCaps struct {
	Durations          []float64 `json:"durations"`
	Resolutions        []string  `json:"resolutions"`
	MaxReferenceImages int       `json:"max_reference_images"`
	FirstFrame         bool      `json:"first_frame"`
	LastFrame          bool      `json:"last_frame"`
	NativeAudio        bool      `json:"native_audio"`
}

// Info is a model the screen can choose, with what it can do and why it cannot be chosen now.
type Info struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Media  string `json:"media"`
	Source string `json:"source"`
	// Available is false when the model cannot be called now; Unavailable is the reason code (domain.ExcludedNoBackend,
	// domain.ExcludedCircuitOpen) and UnavailableMessage the reason for the user.
	Available          bool   `json:"available"`
	Unavailable        string `json:"unavailable,omitempty"`
	UnavailableMessage string `json:"unavailable_message,omitempty"`
	// SettingsRoute is the screen that fixes the reason ("/settings/api-keys", "/settings/local-models", "/model-manager"); empty while Available and for a reason
	// that fixes itself (the circuit breaker). A screen links it from "選べません" (#1013).
	SettingsRoute string `json:"settings_route,omitempty"`
	// AcceptsReferences: an image model uses reference images.
	AcceptsReferences bool `json:"accepts_references"`
	// ReferenceMode is how an image model uses references: "edit", "img2img", or "inpaint" (a mask redraws part of one image);
	// empty for a model that takes none (and for a cloud model that does).
	ReferenceMode string `json:"reference_mode,omitempty"`
	// MaxReferenceImages is how many reference images an image model takes (0: not known or none).
	MaxReferenceImages int `json:"max_reference_images,omitempty"`
	// ReferenceRoles are the roles a reference of the model may have (localcfg.Role*; "reference" is the whole picture).
	ReferenceRoles []string `json:"reference_roles,omitempty"`
	// SupportsLoRA: a LoRA can be applied to the model.
	SupportsLoRA bool       `json:"supports_lora,omitempty"`
	Video        *VideoCaps `json:"video,omitempty"`
	// SpeedModes are the ways the model can sample, the default first (a ComfyUI workflow with localcfg.Workflow.SpeedModes: the distilled
	// LoRA of Wan 2.2 14B or Qwen-Image-Edit 2511); empty for a model with one way. A request's speed_mode names one of them.
	SpeedModes []SpeedMode `json:"speed_modes,omitempty" jsonschema:"速さのモード（蒸留 LoRA など）。先頭が既定（default: true）。リクエストの speed_mode に id を渡せる。持たないモデルは警告 speed_mode_unsupported で既定のまま作る。モードがあるモデルのステップ数・cfg はモードが決める（sampling の locked が speed_mode）"`
	// BlockedSpeedModes are the modes of the workflow that the model cannot run now, because the optional file they load is not installed (Catalog.MissingOptional), each with
	// the reason: they are not in SpeedModes, and a request that names one is told why (#1025).
	BlockedSpeedModes []BlockedSpeedMode `json:"blocked_speed_modes,omitempty" jsonschema:"今は使えない速さのモード（そのモードが使う任意ファイル、例: Lightning LoRA が導入されていない）。id・label・reason（日本語の理由）。speed_modes には載らない。リクエストで指定すると警告 speed_mode_unsupported で既定のまま作る"`
	// SupportsSeed: the seed is sent to the model. Without it the n results of a combination are n calls with the same input.
	SupportsSeed bool `json:"supports_seed"`
	// NegativeField: the model has a negative prompt field. Without it the negative prompt is written into the prompt
	// as an "Avoid: ..." sentence.
	NegativeField bool  `json:"negative_field"`
	Price         Price `json:"price"`
	// License is whether the model may be used commercially, with the license, the condition and the source (#928). A model whose
	// license mam has not checked is "unknown".
	License modellicense.Info `json:"license" jsonschema:"商用利用の可否（commercial: yes / conditional / no / unknown）・ライセンス名・条件・出典 URL・確認日。確認できていないモデルは unknown"`
	// Sampling is the steps, cfg, sampler and scheduler the model takes with their defaults (a request's sampling and seed fields);
	// absent for a model that takes none (the cloud models, the distilled LTX-2.5 whose sigmas are fixed).
	Sampling *sampling.Caps `json:"sampling,omitempty" jsonschema:"このモデルが受け取るステップ数・CFG・サンプラー・スケジューラーと既定値。無い項目は受け取らない（指定しても使わない）。steps / cfg の locked が speed_mode のものは、速さのモードが決めるので指定しても無視する"`
	// PromptWeight is whether the model reads the weights of a prompt, "(word:1.3)": supported / weak / unsupported / unknown.
	PromptWeight promptweight.Level `json:"prompt_weight" jsonschema:"プロンプトのウェイト（(word:1.3)）が効くか。supported（効く）/ weak（効くが弱いかもしれない）/ unsupported（効かない: 括弧と数字がそのまま文として読まれる）/ unknown（確かめていない）"`
	// Exposed are the inputs of the model's workflow the screen offers as fields of their own (a request's exposed; Issue #977).
	Exposed []exposed.Cap `json:"exposed,omitempty" jsonschema:"画面に出す値（ComfyUI のノードの入力）。リクエストの exposed に key で値を渡せる。type・min / max・options の範囲外は validation。default はワークフローのグラフが持つ値"`
	// SupportsInpaint: the model redraws the white part of a mask and keeps the rest (reference_mode inpaint).
	SupportsInpaint bool `json:"supports_inpaint,omitempty" jsonschema:"一部だけ描き直す（インペイント）に対応する。reference_mode が inpaint のとき true。対応しないモデルは選べない"`
	// DenoiseDefault is the redraw strength the workflow uses when a request names none (the template's denoise).
	DenoiseDefault *float64 `json:"denoise_default,omitempty" jsonschema:"一部だけ描き直すときの描き直しの強さの既定（0 より大きく 1 以下）。supports_inpaint のモデルだけ"`
	// RequiresInputs are the input images the model cannot run without (first_frame / last_frame / reference): a request without one is stopped before it is sent.
	RequiresInputs []string `json:"requires_inputs,omitempty" jsonschema:"このモデルが必ず使う入力画像（first_frame / last_frame / reference）。I2V のワークフローは first_frame。リクエストに無い（役割が違う）と、送らずに input_missing で止める。無いモデルは省略"`
	// Provider is who serves the model: the vendor of a cloud model ("google", "openai", "kling"...), "chatgpt-subscription" for the ChatGPT subscription's image
	// model, "local" for a ComfyUI / OpenAI-compatible endpoint model or a saved workflow. Origin is "local" or "api". Both are the words of the filter (modelfilter).
	Provider string `json:"provider" jsonschema:"提供元（クラウドはベンダー、local はローカル。絞り込みの providers に使う）"`
	Origin   string `json:"origin" jsonschema:"local（この PC・登録した接続先）/ api（クラウド・サブスク）"`
	// Inputs are what the model takes as input (traits.Input: t2i, i2i, t2v, i2v, first_frame, reference_face...), derived from the same capabilities as
	// reference_mode, reference_roles and video, so the two never disagree.
	Inputs []traits.Input `json:"inputs" jsonschema:"入力の種類（t2i / i2i / inpaint / edit / t2v / i2v / first_frame / last_frame / reference_whole / reference_face / reference_style / reference_pose / reference_control）。参照の使い方・動画の能力と同じ元から導く"`
	// Catalog is what the model manager's catalog says of a model it installed (adult, the traits, the VRAM guideline); nil for any other model.
	Catalog *CatalogTraits `json:"catalog,omitempty" jsonschema:"モデルマネージャーのカタログのモデルだけ。成人向けか・非検閲か・パラメータ数・速度と品質の目安・VRAM の目安"`
}

// FilterByCommercial keeps the models whose answer the filter allows.
func FilterByCommercial(models []Info, f modellicense.Filter) []Info {
	out := make([]Info, 0, len(models))
	for _, m := range models {
		if f.Allows(m.License.Commercial) {
			out = append(out, m)
		}
	}
	return out
}

// Catalog lists the models of the generation test.
type Catalog struct {
	Adapter *modeladapter.Adapter
	// Hidden says a model is left out of the lists and cannot be chosen (the adult models of the model manager while the age
	// gate is closed). Nil hides nothing.
	Hidden func(id string) bool
	// HideChoices leaves the choices of a choice input that only an adult model has out of Info.Exposed while the gate is closed. Nil hides none.
	HideChoices func(options []string, keep string) []string
	// MissingNodes gives the reason a local model cannot be chosen because the custom nodes its workflow requires (localcfg.Workflow.RequiresNodes) are
	// not installed; empty when it can. Nil asks for none.
	MissingNodes func(id string, required []string) string
	// CatalogInfo says what the model manager's catalog knows of a local model by its identifier (modelmanager.Service.InfoOf); nil knows none.
	CatalogInfo func(id string) (modelfilter.CatalogInfo, bool)
}

// Models lists the models of the media ("" for both): every built-in cloud model, the models of the registered endpoints,
// the saved ComfyUI workflows and the ChatGPT subscription's image model while it is on. A model that cannot be called
// now is listed with the reason.
func (c Catalog) Models(media string) []Info {
	cfg := c.Adapter.Config()
	out := []Info{}
	if media != gentestrepo.MediaVideo {
		for _, m := range domain.ImageModels() {
			if !c.hidden(m) {
				out = append(out, c.info(cfg, m, gentestrepo.MediaImage))
			}
		}
	}
	if media != gentestrepo.MediaImage {
		for _, m := range domain.VideoModels() {
			if !c.hidden(m) {
				out = append(out, c.info(cfg, m, gentestrepo.MediaVideo))
			}
		}
	}
	return out
}

// ExcludedMissingNode is the reason code of a model whose workflow needs a custom node that is not installed (Info.Unavailable).
const ExcludedMissingNode = "missing_node"

// missingNodes is the reason the workflow of a local model cannot run: the nodes it requires are not installed ("" when it can).
func (c Catalog) missingNodes(cfg modeladapter.Config, m domain.ModelName) string {
	lm, _, ok := cfg.LocalModel(m)
	if !ok || c.MissingNodes == nil || lm.Workflow == nil {
		return ""
	}
	return c.MissingNodes(string(m), lm.Workflow.RequiresNodes)
}

// exposedCaps are the exposed inputs of the workflow, with the choices the closed age gate hides left out (the value the graph holds stays).
func (c Catalog) exposedCaps(w localcfg.Workflow) []exposed.Cap {
	caps := w.ExposedCaps()
	if c.HideChoices == nil {
		return caps
	}
	for i, cp := range caps {
		if len(cp.Options) == 0 {
			continue
		}
		d, _ := cp.Default.(string)
		caps[i].Options = c.HideChoices(cp.Options, d)
	}
	return caps
}

func (c Catalog) hidden(m domain.ModelName) bool { return c.Hidden != nil && c.Hidden(string(m)) }

// Model returns one model of the media; false when there is none. Only that model is described (its backend asked, its price and
// capabilities read) and only it goes through the Hidden predicate: describing every model of the list to pick one out cost a key
// lookup in the OS secure storage (a call that can wait for its time limit) and a read of the workflow graph for each of them, on
// every estimate of a match or a batch (#981).
func (c Catalog) Model(media, id string) (Info, bool) {
	cfg := c.Adapter.Config()
	for _, kind := range []struct {
		media  string
		models []domain.ModelName
	}{{gentestrepo.MediaImage, domain.ImageModels()}, {gentestrepo.MediaVideo, domain.VideoModels()}} {
		if media != "" && media != kind.media {
			continue
		}
		for _, m := range kind.models {
			if string(m) == id && !c.hidden(m) {
				return c.info(cfg, m, kind.media), true
			}
		}
	}
	return Info{}, false
}

func (c Catalog) info(cfg modeladapter.Config, m domain.ModelName, media string) Info {
	mi := Info{
		ID: string(m), Label: modelreg.Label(string(m)), Media: media, Source: sourceOf(cfg, m),
		SupportsSeed: direct.SupportsSeed(cfg, m), NegativeField: direct.NegativeField(cfg, m), Price: priceOf(cfg, m, media),
		License: modellicense.Of(string(m)),
	}
	catalogInfo, known := c.catalogInfo(string(m)) // once: it stats the model's files
	var asked *modelfilter.CatalogInfo
	if known {
		asked = &catalogInfo
	}
	a := c.availability(cfg, m, mi.Source, asked)
	mi.Available, mi.Unavailable, mi.UnavailableMessage = a.Available, string(a.Reason), a.Message
	mi.SettingsRoute = a.Route
	mi.Provider, mi.Origin = providerOf(m, mi.Source), originOf(mi.Source)
	if known {
		mi.Catalog = &CatalogTraits{Adult: catalogInfo.Adult, Traits: catalogInfo.Traits, VRAMMinGB: catalogInfo.VRAMMinGB, MissingOptional: catalogInfo.MissingOptional, PartialNote: catalogInfo.PartialNote}
		if catalogInfo.Name != "" {
			mi.Label = catalogInfo.Name // the catalog's name, not the identifier (#1023)
		}
	}
	if _, _, name, ok := localcfg.ParseID(string(m)); ok && mi.Label == mi.ID {
		mi.Label = name // a local model the catalog does not know (an external ComfyUI's, one of the user's): its own name, not "comfyui:<endpoint>/<name>" (#1023)
	}
	mi.PromptWeight = promptweight.Unknown
	if lm, _, ok := cfg.LocalModel(m); ok && lm.Workflow != nil {
		mi.Sampling, mi.PromptWeight = lm.Workflow.SamplingCaps(), lm.Workflow.PromptWeight.OrUnknown()
		mi.Exposed = c.exposedCaps(*lm.Workflow)
		mi.SpeedModes, mi.BlockedSpeedModes = SpeedModesOf(*lm.Workflow, catalogInfo.MissingOptional)
		mi.RequiresInputs = lm.Workflow.RequiredImages()
		if d, ok := denoiseDefault(lm.Workflow); ok {
			mi.SupportsInpaint, mi.DenoiseDefault = true, &d
		}
	}
	if media == gentestrepo.MediaImage {
		mi.AcceptsReferences = cfg.AcceptsImages(m)
		mi.SupportsLoRA = SupportsLoRA(cfg, m)
		if caps := ReferenceCapsOf(cfg, m); caps.Max > 0 {
			mi.ReferenceMode, mi.MaxReferenceImages, mi.ReferenceRoles = string(caps.Mode), caps.Max, slices.Clone(caps.Roles)
		} else if mi.AcceptsReferences {
			mi.ReferenceRoles = []string{localcfg.RoleWhole} // a model whose references are not described: the whole picture
		}
		mi.Inputs = inputsOf(mi)
		return mi
	}
	if caps, ok := cfg.VideoCaps(m); ok {
		mi.Video = &VideoCaps{
			Durations: slices.Clone(caps.Durations), Resolutions: slices.Clone(caps.Resolutions), MaxReferenceImages: caps.MaxReferenceImages,
			FirstFrame: caps.FirstFrame, LastFrame: caps.LastFrame, NativeAudio: caps.NativeAudio,
		}
	}
	mi.Inputs = inputsOf(mi)
	return mi
}

func sourceOf(cfg modeladapter.Config, m domain.ModelName) string {
	if _, ok := cfg.Workflows[string(m)]; ok {
		return SourceWorkflow
	}
	if chatgptid.Is(m) {
		return SourceChatGPT
	}
	if _, _, ok := cfg.LocalModel(m); ok {
		return SourceLocal
	}
	return SourceBuiltin
}

// NoKeyMessage is the unavailable_message of a cloud model whose API key is not registered (#1013).
const NoKeyMessage = "API キーが登録されていません（設定の「API キー」）"

func unavailableMessage(reason, source string) string {
	switch {
	case reason == agegate.Excluded:
		return "年齢確認がオフのため、成人向けモデルは使えません（モデル管理で年齢確認をオンにしてください）"
	case reason == domain.ExcludedCircuitOpen:
		return "このモデルのベンダーで失敗が続いたため、しばらく止めています。少し待ってからもう一度お試しください"
	case source == SourceChatGPT:
		return "ChatGPT にログインしていないか、画像生成が OFF です（設定の「ChatGPT でログイン」）"
	case source == SourceLocal || source == SourceWorkflow:
		return "接続先を使えません（設定の「ローカル・代替モデル」を確認してください）"
	}
	return NoKeyMessage
}
