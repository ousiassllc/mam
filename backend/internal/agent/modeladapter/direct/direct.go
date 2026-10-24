// Package direct is the direct call of the Model Adapter (spec/architecture/gentest/gentest-adapter.md): it generates with
// exactly the model it is given, sends the prompt as it is and never switches to another model. The generation test screen
// calls it because a comparison of models means nothing when a failing model is replaced. The call goes through
// modeladapter.Call, so the vendor's circuit breaker, concurrency limit, retry and timeout apply as they do to the Adapter's
// own calls. It is a package of its own because package modeladapter is over its line limit.
package direct

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/shotprompt"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The refusals of a request that the model cannot take. They come before any vendor call: nothing is billed.
var (
	// ErrNoBackend: the model has no backend (it is not a model of the running setting).
	ErrNoBackend = errors.New("direct: the model has no backend")
	// ErrInputsUnsupported: an input image the model cannot use (the generation test drops those before it calls).
	ErrInputsUnsupported = errors.New("direct: the model cannot use the input images")
	// ErrInputsConflict: input images that cannot be combined (first frame and free references, a last frame alone).
	ErrInputsConflict = errors.New("direct: the input images cannot be combined")
	// ErrResolution: the model does not produce the resolution.
	ErrResolution = errors.New("direct: the model does not produce the resolution")
)

// Image makes one image with model. req is sent as it is: no fail-over, and a request with input images for a model that
// takes none is refused (ErrInputsUnsupported) instead of being sent without them. A seed is dropped for a model that takes
// none (SupportsSeed). The output records the model, the estimated cost (Config.ImageCost) and FellBack false.
func Image(ctx context.Context, a *modeladapter.Adapter, model domain.ModelName, req modeladapter.ImageRequest) (modeladapter.ImageOutput, error) {
	cfg, backends := a.Snapshot()
	backend := backends.Image[model]
	if backend == nil {
		return modeladapter.ImageOutput{}, fmt.Errorf("%w: %s", ErrNoBackend, model)
	}
	if len(req.Inputs) > 0 && !cfg.AcceptsImages(model) {
		return modeladapter.ImageOutput{}, fmt.Errorf("%w: %s", ErrInputsUnsupported, model)
	}
	if req.AspectRatio == "" {
		req.AspectRatio = domain.DefaultAspectRatio
	}
	if !SupportsSeed(cfg, model) {
		req.Seed = nil
	}
	if !NegativeField(cfg, model) {
		req.NegativePrompt = ""
	}
	apiModel := cfg.ModelSettings(model).Model
	res, err := modeladapter.Call(ctx, a, modeladapter.VendorOf(model), nil, string(model), func(ctx context.Context) (modeladapter.ImageResult, error) {
		return backend.GenerateImage(ctx, apiModel, req)
	})
	if err != nil {
		return modeladapter.ImageOutput{}, err
	}
	return modeladapter.ImageOutput{ImageResult: res, Model: model, EstimatedCost: cfg.ImageCost(model)}, nil
}

// Video makes one clip with the model of target at the resolution of target. The length is snapped to one the model
// produces (VideoCapabilities.SnapDuration; the output says which). The inputs are checked against the model's capabilities
// and refused when it cannot take them (ErrInputsUnsupported, ErrInputsConflict), and so is a resolution it does not produce
// (ErrResolution). A seed or a negative prompt for a model without that field is dropped. There is no time limit beyond the vendor's timeout.
func Video(ctx context.Context, a *modeladapter.Adapter, target modeladapter.VideoTarget, req modeladapter.VideoRequest) (modeladapter.VideoOutput, error) {
	cfg, backends := a.Snapshot()
	caps, ok := cfg.VideoCaps(target.Model)
	if !ok {
		return modeladapter.VideoOutput{}, fmt.Errorf("%w: %s is not a video model", ErrNoBackend, target.Model)
	}
	backend := backends.Video[target.Model]
	if backend == nil {
		return modeladapter.VideoOutput{}, fmt.Errorf("%w: %s", ErrNoBackend, target.Model)
	}
	if !caps.SupportsResolution(target.Resolution) {
		return modeladapter.VideoOutput{}, fmt.Errorf("%w: %s %s", ErrResolution, target.Model, target.Resolution)
	}
	if err := checkInputs(caps, req); err != nil {
		return modeladapter.VideoOutput{}, fmt.Errorf("%w: %s", err, target.Model)
	}
	req.DurationSeconds = caps.SnapDuration(req.DurationSeconds)
	req.Resolution = target.Resolution
	if req.AspectRatio == "" {
		req.AspectRatio = domain.DefaultAspectRatio
	}
	if !SupportsSeed(cfg, target.Model) {
		req.Seed = nil
	}
	if !NegativeField(cfg, target.Model) {
		req.NegativePrompt = ""
	}
	apiModel := cfg.ModelSettings(target.Model).Model
	res, err := modeladapter.Call(ctx, a, modeladapter.VendorOf(target.Model), nil, string(target.Model), func(ctx context.Context) (modeladapter.VideoResult, error) {
		return backend.GenerateVideo(ctx, apiModel, req)
	})
	if err != nil {
		return modeladapter.VideoOutput{}, err
	}
	return modeladapter.VideoOutput{
		VideoResult: res, Model: target.Model, Resolution: target.Resolution, DurationSeconds: req.DurationSeconds,
		Request: req, EstimatedCost: cfg.VideoCost(target, req.DurationSeconds),
	}, nil
}

// checkInputs refuses the inputs the capabilities do not allow. A model reads either the frames or the free references,
// never both (Seedance 2.0 rejects a request that mixes the scenarios), and a last frame needs a first frame.
func checkInputs(caps modeladapter.VideoCapabilities, req modeladapter.VideoRequest) error {
	switch {
	case req.FirstFrame != nil && !caps.FirstFrame, req.LastFrame != nil && !caps.LastFrame:
		return ErrInputsUnsupported
	case len(req.References) > caps.MaxReferenceImages:
		return ErrInputsUnsupported
	case req.LastFrame != nil && req.FirstFrame == nil:
		return ErrInputsConflict
	case len(req.References) > 0 && (req.FirstFrame != nil || req.LastFrame != nil):
		return ErrInputsConflict
	}
	return nil
}

// SupportsSeed reports whether a call of the model uses the seed it is given: FLUX.2 and Runway Gen-4.5 at the vendor, and
// a ComfyUI model whose workflow has a seed assignment. For any other model the generation test calls n times with the same
// input and records no seed.
func SupportsSeed(cfg modeladapter.Config, m domain.ModelName) bool {
	if lm, kind, ok := cfg.LocalModel(m); ok {
		return kind == localcfg.KindComfyUI && lm.Workflow != nil && lm.Workflow.Has(localcfg.ParamSeed)
	}
	return modeladapter.SupportsSeed(m)
}

// NegativeField reports whether the model has a negative prompt field: Kling 3.0, and a ComfyUI model whose workflow has a
// negative prompt assignment. For any other model the negative prompt is folded into the prompt (Fold).
func NegativeField(cfg modeladapter.Config, m domain.ModelName) bool {
	if lm, kind, ok := cfg.LocalModel(m); ok {
		return kind == localcfg.KindComfyUI && lm.Workflow != nil && lm.Workflow.Has(localcfg.ParamNegativePrompt)
	}
	return m == domain.ModelKling3
}

// Fold writes the negative prompt into the prompt as an "Avoid: ..." sentence, the wording the Shot prompts use
// (shotprompt.AvoidSentence). An empty negative prompt leaves the prompt as it is.
func Fold(prompt, negative string) string {
	negative = strings.TrimRight(strings.TrimSpace(negative), ". ")
	if negative == "" {
		return prompt
	}
	return prompt + shotprompt.AvoidSentence([]string{negative})
}

// AspectProblem says why a video model refuses the aspect ratio (empty when it takes it): Runway Gen-4.5 makes 16:9 and 9:16,
// and 1:1 only from a first frame (its backend answers 422 otherwise; the Adapter's shot path would switch to the alternative,
// a direct call fails). Other models take any ratio the caller names.
func AspectProblem(m domain.ModelName, aspect string, hasFirstFrame bool) string {
	if m != domain.ModelRunwayGen45 {
		return ""
	}
	switch {
	case aspect == domain.AspectRatio16x9 || aspect == domain.AspectRatio9x16:
		return ""
	case aspect == domain.AspectRatio1x1 && !hasFirstFrame:
		return "1:1 は最初のフレームがあるときだけ作れます"
	case aspect == domain.AspectRatio1x1:
		return ""
	}
	return "16:9・9:16・1:1（最初のフレームつき）だけ作れます"
}
