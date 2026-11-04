package gentest

import (
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/exposedreq"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/inputcheck"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
)

// Warning is something the confirmation tells the user before the batch runs: an input a model cannot use, a length that is
// rounded, a request the model will refuse. Model is "" for a warning about the whole batch.
type Warning struct {
	Model   string `json:"model,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// Roles are the roles of the references the model does not take, for WarnRoleUnsupported only: they are used as a whole picture.
	Roles []string `json:"roles,omitempty"`
}

// The codes of a Warning.
const (
	WarnInputsDropped    = refs.WarnInputsDropped        // an input image the model cannot use is left out for that model
	WarnDurationSnapped  = "duration_snapped"            // the length is rounded to one the model makes
	WarnResolution       = "resolution"                  // the model does not make the resolution: its results fail
	WarnAspect           = "aspect_ratio"                // the model refuses the aspect ratio: its results fail
	WarnNoPrice          = "no_price"                    // the price is unknown: the model is left out of the total
	WarnNegativeFolded   = "negative_folded"             // the model has no negative prompt field: it is written into the prompt
	WarnSeedUnsupported  = "seed_unsupported"            // the model takes no seed: the n results are the same call n times
	WarnFramesOverFree   = inputcheck.WarnFramesOverFree // first / last frames and free references cannot be used together
	WarnTooManyReference = refs.WarnTooMany              // more references than the model takes
	WarnRoleUnsupported  = refs.WarnRoleUnsupported      // the model does not take the role of a reference: it is used as a whole
	WarnLoRAUnsupported  = refs.WarnLoRAUnsupported      // the model cannot take a LoRA: it is not sent
	WarnSpeedUnsupported = refs.WarnSpeedUnsupported     // the model has no such speed mode: it runs its default
	// WarnSamplingIgnored and WarnSeedIgnored (sampling.go): the model does not take the steps, cfg, sampler, scheduler or seed asked for.
	// WarnPromptWeight (promptweight.go): the prompt has weights and the model does not read them (or reads them poorly).
)

// modelPlan is how one model takes the request: the inputs it uses, the parameters it is called with and its unit price.
type modelPlan struct {
	info       modelcatalog.Info
	resolution string
	seconds    float64
	// used are the indexes of the request's References this model is given.
	used []int
	// lora says the request's LoRA is sent to the model, avoidPose that the pose negative is added to its negative prompt.
	lora, avoidPose bool
	// speedMode is the speed mode the model is asked for ("" for its default).
	speedMode string
	// sampling is the steps, cfg, sampler and scheduler the model runs with (what it takes: the request's value, else its default); nil when it takes none.
	sampling *sampling.Settings
	// exposed are the values the model's exposed inputs run with (nil: it exposes none).
	exposed map[string]any
	// denoise is the redraw strength sent to an inpaint model (nil: the graph's own, or not an inpaint run).
	denoise *float64
	// unused are the input images of the request this model is not given, with why (a result records them: record.UnusedInput).
	unused   []inputcheck.Unused
	warnings []Warning
	basis    string
	// unit is the price of one result in USD (nil when the basis is unknown).
	unit *float64
}

func (s *Service) planModel(cfg modeladapter.Config, r Request, info modelcatalog.Info) modelPlan {
	p := modelPlan{info: info}
	warn := func(code, format string, args ...any) {
		p.warnings = append(p.warnings, Warning{Model: info.ID, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	if r.Media == gentestrepo.MediaImage {
		s.planImage(r, &p)
	} else {
		s.planVideo(cfg, r, &p, warn)
	}
	planSpeedMode(r, &p, warn)
	planSampling(r, &p, warn)
	planInpaint(r, &p, warn)
	var ignored []string
	if p.exposed, ignored = exposedreq.Plan(info, r.Exposed); len(ignored) > 0 {
		warn(WarnExposedIgnored, "%s", exposedreq.Warning(info.Label, ignored))
	}
	planPromptWeight(r, &p, warn)
	p.unit, p.basis = unitPrice(info, r.Media, p.resolution, p.seconds)
	if r.Media == gentestrepo.MediaImage && p.basis == modelcatalog.BasisPriced {
		// the price of an image can depend on its size (GPT Image 2): the catalog holds the dearest one
		unit := cfg.ImageCostFor(domain.ModelName(info.ID), r.AspectRatio)
		p.unit = &unit
	}
	if p.basis == modelcatalog.BasisUnknown {
		warn(WarnNoPrice, "%s は単価が分からないため、見積りの合計に入れません", info.Label)
	}
	if (hasNegative(r) || p.avoidPose) && !info.NegativeField {
		warn(WarnNegativeFolded, "%s にはネガティブプロンプトの欄が無いため、プロンプトに「Avoid: …」として書き足して送ります", info.Label)
	}
	if r.Count > 1 && !info.SupportsSeed {
		warn(WarnSeedUnsupported, "%s はシードを渡せないため、%d 本は同じ入力での %d 回の呼び出しです（結果は似るだけで同じにはなりません）", info.Label, r.Count, r.Count)
	}
	return p
}

func hasNegative(r Request) bool {
	return slices.ContainsFunc(r.Prompts, func(p PromptInput) bool { return p.Negative != "" })
}

// planVideo decides the resolution, the length and the inputs of a video model.
func (s *Service) planVideo(cfg modeladapter.Config, r Request, p *modelPlan, warn func(code, format string, args ...any)) {
	caps, _ := cfg.VideoCaps(domain.ModelName(p.info.ID))
	p.resolution = r.Resolution
	if p.resolution == "" {
		p.resolution = caps.Resolutions[0]
		if caps.SupportsResolution("720p") {
			p.resolution = "720p"
		}
	} else if !caps.SupportsResolution(p.resolution) {
		warn(WarnResolution, "%s は解像度 %s を作れません（%v）。このモデルの結果は失敗します", p.info.Label, p.resolution, caps.Resolutions)
	}
	p.seconds = caps.SnapDuration(r.DurationSeconds)
	if p.seconds != r.DurationSeconds {
		warn(WarnDurationSnapped, "%s の長さは %g 秒から %g 秒に丸められます", p.info.Label, r.DurationSeconds, p.seconds)
	}
	in := inputcheck.Video(caps, p.info.Label, rolesOf(r))
	p.applyInputs(in)
	if msg := direct.AspectProblem(domain.ModelName(p.info.ID), r.AspectRatio, in.FirstFrame); msg != "" {
		warn(WarnAspect, "%s: 縦横比 %s は使えません（%s）。このモデルの結果は失敗します", p.info.Label, r.AspectRatio, msg)
	}
}

// planSpeedMode decides the speed mode the model is asked for: the one of the request when the model has it, else its default (with a warning).
func planSpeedMode(r Request, p *modelPlan, warn func(code, format string, args ...any)) {
	switch {
	case r.SpeedMode == "":
	case p.info.HasSpeedMode(r.SpeedMode):
		p.speedMode = r.SpeedMode
	default:
		warn(WarnSpeedUnsupported, "%s", p.info.SpeedModeUnsupported(r.SpeedMode))
	}
}

// unitPrice is the price of one result of the model: its basis and, when the price is known, the USD.
func unitPrice(info modelcatalog.Info, media, resolution string, seconds float64) (*float64, string) {
	zero := 0.0
	switch info.Price.Basis {
	case modelcatalog.BasisFree:
		return &zero, modelcatalog.BasisFree
	case modelcatalog.BasisPriced:
		if media == gentestrepo.MediaImage && info.Price.PerImageUSD != nil {
			v := *info.Price.PerImageUSD
			return &v, modelcatalog.BasisPriced
		}
		if per, ok := info.Price.PerSecondUSD[resolution]; ok && media == gentestrepo.MediaVideo {
			v := per * seconds
			return &v, modelcatalog.BasisPriced
		}
	}
	return nil, modelcatalog.BasisUnknown
}

// resultParamsOf is what is recorded of the request for the result of one model.
func resultParamsOf(r Request, mp modelPlan, translation *record.SentText, maskID string, inputIDs []string) record.ResultParams {
	p := record.ResultParams{
		AspectRatio: r.AspectRatio, Resolution: mp.resolution, DurationSeconds: mp.seconds, Translation: translation, AvoidPose: mp.avoidPose, SpeedMode: mp.speedMode, Sampling: mp.sampling, Exposed: mp.exposed,
	}
	if r.Mask != nil && len(r.References) == 1 {
		p.Inpaint = &record.Inpaint{Denoise: mp.denoise, SourceKind: r.References[0].Source, SourceID: r.References[0].ID, MaskInput: maskID}
	}
	if mp.lora {
		p.LoRA = &refs.LoRA{Name: r.LoRA.Name, Strength: *r.LoRA.Strength}
	}
	for _, u := range mp.unused {
		p.UnusedInputs = append(p.UnusedInputs, record.UnusedInput{InputID: inputIDs[u.Index], Role: r.References[u.Index].Role, Reason: u.Reason})
	}
	return p
}

// planInpaint decides the redraw strength an inpaint model is sent. A model that has no denoise keeps the graph's value and warns.
func planInpaint(r Request, p *modelPlan, warn func(code, format string, args ...any)) {
	if r.Mask == nil {
		return
	}
	if r.Denoise != nil && p.info.DenoiseDefault == nil {
		warn(WarnDenoiseIgnored, "%s は描き直しの強さを受け取れないため、テンプレートの既定で作ります", p.info.Label)
		return
	}
	if r.Denoise != nil {
		p.denoise = r.Denoise
		return
	}
	p.denoise = p.info.DenoiseDefault
}

// planImage decides which references, which LoRA and which pose option an image model takes of the request (refs.Decide).
func (s *Service) planImage(r Request, p *modelPlan) {
	roles := make([]string, len(r.References))
	for i, ref := range r.References {
		roles[i] = ref.Role
	}
	info := p.info
	d := refs.Decide(refs.Model{
		Label: info.Label, AcceptsReferences: info.AcceptsReferences, MaxReferences: info.MaxReferenceImages, Roles: info.ReferenceRoles,
		SupportsLoRA: info.SupportsLoRA,
	}, roles, r.LoRA != nil, r.AvoidReferencePose)
	p.used, p.lora, p.avoidPose = d.Used, d.LoRA, d.AvoidPose
	for _, w := range d.Warnings {
		p.warnings = append(p.warnings, Warning{Model: info.ID, Code: w.Code, Message: w.Message, Roles: w.Roles})
	}
	p.unused = inputcheck.ImageUnused(len(r.References), p.used, info.AcceptsReferences, info.MaxReferenceImages)
}
