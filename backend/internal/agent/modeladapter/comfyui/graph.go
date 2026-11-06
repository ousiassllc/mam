package comfyui

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/aspectsize"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/sampled"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/stage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/imgprep"
)

// defaultNegative is what an image workflow's negative prompt gets: an ImageRequest has none.
const defaultNegative = "blurry, low quality, distorted, deformed, extra fingers, watermark, text artifacts"

// values are what a generation puts into the workflow, before the files are uploaded.
type values struct {
	prompt, negative string
	seed             int64
	width, height    int
	frames, fps      int
	references       []modeladapter.Input
	first, last      *modeladapter.Input
	opts             localcfg.ImageOptions
	mask             *modeladapter.Input // inpaint mask (nil: none); redraw is its strength (nil: the graph's denoise)
	redraw           *float64
	denoiseFloor     float64                   // the workflow's ReferenceDenoiseMin
	speedMode        string                    // the request's speed mode ("": the workflow's first)
	sampling         localcfg.SamplingSettings // the request's sampling values; build keeps the ones the workflow takes
	upscaleRatio     float64                   // the workflow's Upscale.Ratio of the request's UpscaleScale (build sets it)
	source           *modeladapter.Input       // the video a video enhancement workflow takes (uploaded like an image; the extension tells the server it is a video)
	videoScale       float64                   // the factor an upscale video workflow enlarges by
	frameMultiplier  int                       // how many frames an interpolation workflow makes of one
	outputFPS        float64                   // the frame rate of the video an interpolation workflow makes
	keepSlots        bool                      // no image is uploaded (GraphFor): the image nodes a workflow cannot run without keep the file the workflow holds
}

// imageValues are the values of an image request. A reference-mode workflow uses Inputs only when Options.Refs describes them;
// bare Inputs (a picture to see, not to copy) are ignored and the run is text-to-image. Without a reference mode, Inputs are used as given.
func (b *Backend) imageValues(req modeladapter.ImageRequest) values {
	w, h := aspectsize.Flux(req.AspectRatio)
	negative := defaultNegative
	if req.NegativePrompt != "" {
		negative = req.NegativePrompt
	}
	v := values{prompt: req.Prompt, negative: negative, seed: seedOr(req.Seed), width: w, height: h, references: req.Inputs, opts: req.Options, sampling: req.Options.Sampling, speedMode: req.Options.SpeedMode}
	if in := req.Inpaint; in != nil {
		mask := in.Mask
		v.mask, v.redraw = &mask, in.Denoise
	}
	if wf := b.Model.Workflow; wf != nil && wf.ReferenceMode != "" && len(req.Options.Refs) == 0 {
		v.references = nil
	}
	return v
}

func videoValues(wf localcfg.Workflow, req modeladapter.VideoRequest) values {
	w, h := localcfg.VideoSize(req.Resolution, req.AspectRatio, wf.SizeMultiple)
	return values{
		prompt: req.Prompt, negative: req.NegativePrompt, seed: seedOr(req.Seed), width: w, height: h,
		frames: wf.Frames(req.DurationSeconds), fps: wf.FPSOrDefault(),
		references: req.References, first: req.FirstFrame, last: req.LastFrame, speedMode: req.SpeedMode, sampling: req.Sampling,
	}
}

// seedOr is the seed the request names, else a new one.
func seedOr(s *int64) int64 {
	if s != nil {
		return *s
	}
	return newSeed()
}

// newSeed is not a secret: it only varies the picture.
func newSeed() int64 { return rand.Int64N(1 << 50) } //nolint:gosec // a sampler seed, not a credential

// input is the image an assignment needs, nil when the assignment is not about an image.
func (v values) input(a localcfg.Assignment) (in *modeladapter.Input, isImage bool) {
	switch a.Param {
	case localcfg.ParamReferenceImage:
		if a.Index < len(v.references) {
			return &v.references[a.Index], true
		}
		return nil, true
	case localcfg.ParamFirstFrame:
		return v.first, true
	case localcfg.ParamLastFrame:
		return v.last, true
	case localcfg.ParamMaskImage:
		return v.mask, true
	case localcfg.ParamSourceVideo:
		return v.source, true
	}
	return nil, false
}

// scalar is the JSON value of an assignment that is not an image.
func (v values) scalar(p localcfg.Param) any {
	switch p {
	case localcfg.ParamPrompt:
		return v.prompt
	case localcfg.ParamNegativePrompt:
		return v.negative
	case localcfg.ParamSeed:
		return v.seed
	case localcfg.ParamWidth:
		return v.width
	case localcfg.ParamHeight:
		return v.height
	case localcfg.ParamFrames:
		return v.frames
	case localcfg.ParamFPS:
		return v.fps
	case localcfg.ParamReferenceDenoise:
		return v.denoise()
	case localcfg.ParamDenoise:
		if v.redraw == nil {
			return nil
		}
		return *v.redraw
	case localcfg.ParamUpscaleRatio:
		return v.upscaleRatio
	case localcfg.ParamVideoScale:
		return v.videoScale
	case localcfg.ParamFrameMultiplier:
		return v.frameMultiplier
	case localcfg.ParamOutputFPS:
		return v.outputFPS
	case localcfg.ParamReferenceStrength:
		return v.opts.Ref(0).StrengthOrDefault()
	}
	return nil
}

// missing names the first image an assignment of the workflow needs and the request does not have: the
// workflow cannot run without it (an assignment is a required input), so the request is refused at once and
// the Adapter routes to the alternative model.
func (b *Backend) missing(v values) error {
	for _, a := range b.Model.Workflow.Assignments {
		if in, isImage := v.input(a); isImage && in == nil && !a.Optional && !b.noReferenceOK(v, a) {
			return b.Client.Error(http.StatusUnprocessableEntity, fmt.Sprintf("the workflow needs %s (%s)%s, the request has none", a.Param, a.Node, b.referenceRole()), nil)
		}
	}
	return nil
}

// referenceRole names what the reference image is for in a ControlNet or an IP-Adapter workflow (they cannot run without it), "" otherwise.
func (b *Backend) referenceRole() string {
	switch b.Model.Workflow.ReferenceMode {
	case localcfg.ModeControlNet:
		return " as the control image of its ControlNet"
	case localcfg.ModeIPAdapter:
		return " as the image its IP-Adapter takes the look from"
	}
	return ""
}

// build puts the values into a copy of the workflow's graph. files are the names the images were uploaded
// as, by assignment index.
func (b *Backend) build(v values, files map[int]string) (json.RawMessage, error) {
	v.denoiseFloor = b.Model.Workflow.ReferenceDenoiseMin
	if u := b.Model.Workflow.Upscale; u != nil {
		v.upscaleRatio = u.Ratio(v.opts.UpscaleScale)
	}
	if err := sampled.Check(*b.Model.Workflow, v.sampling); err != nil {
		return nil, b.Client.Error(http.StatusUnprocessableEntity, err.Error(), nil)
	}
	v.sampling = sampled.Applicable(*b.Model.Workflow, v.sampling)
	if n := b.Model.Workflow.Count(localcfg.ParamReferenceImage); b.Model.Workflow.ReferenceMode != "" && len(v.references) > n {
		v.references = v.references[:n] // the images beyond the slots were not uploaded
	}
	g, err := b.Model.Workflow.GraphWith(v.opts.Exposed) // the values of the exposed inputs (Issue #977) go in first; the assignments below never touch those inputs
	if err != nil {
		return nil, b.Client.Error(http.StatusUnprocessableEntity, "the workflow is unusable: "+err.Error(), nil)
	}
	for i, a := range b.Model.Workflow.Assignments {
		value := v.scalar(a.Param)
		if a.Param == localcfg.ParamFrames {
			value = a.FramesFor(v.frames) // the frame count with the assignment's offset (frames-2)
		}
		if sv, named, isSampling := sampled.Value(a, v.sampling); isSampling {
			if !named {
				continue // the request names no value: the graph's own stays
			}
			value = sv
		}
		name, isFile := files[i]
		if isFile {
			value = name
		} else if _, isImage := v.input(a); (isImage && v.keepSlots) || a.Param == localcfg.ParamReferenceImage || (a.Param == localcfg.ParamDenoise && v.redraw == nil) {
			continue // a slot the run has no image for (GraphFor keeps the workflow's file), or a denoise the request leaves at the graph's value
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, b.Client.Error(http.StatusUnprocessableEntity, fmt.Sprintf("the %s value cannot be put into the workflow: %v", a.Param, err), nil)
		}
		g[a.Node].Inputs[a.Input] = raw
	}
	if err := applySpeedMode(g, *b.Model.Workflow, v.speedMode); err != nil {
		return nil, b.Client.Error(http.StatusUnprocessableEntity, "the workflow is unusable: "+err.Error(), nil)
	}
	dropUnused(g, *b.Model.Workflow, v)
	applyLoRA(g, b.Model.Workflow.LoraModelNode, v.opts.LoRA)
	if err := stage.Apply(g, *b.Model.Workflow, len(v.references), v.opts, v.prompt); err != nil {
		return nil, b.Client.Error(http.StatusUnprocessableEntity, "the workflow is unusable: "+err.Error(), nil)
	}
	return json.Marshal(g)
}

// upload sends every image the workflow takes to the server (POST /upload/image keeps any bytes under a
// name with the extension it was given) and returns the names to put into the graph, by assignment index.
func (b *Backend) upload(ctx context.Context, v values, promptID string) (map[int]string, error) {
	files := map[int]string{}
	for i, a := range b.Model.Workflow.Assignments {
		in, isImage := v.input(a)
		if !isImage || in == nil {
			continue // not an image, or a slot the run has no image for (missing refused a required one)
		}
		if kind := b.Model.Workflow.ReferencePreprocess; kind != "" && a.Param == localcfg.ParamReferenceImage {
			data, mime, err := imgprep.Apply(kind, in.Data, in.MimeType) // the control image is made here, not by a custom node (Issue #993)
			if err != nil {
				return nil, b.Client.Error(http.StatusUnprocessableEntity, "the reference image cannot be turned into the control image: "+err.Error(), nil)
			}
			in = &modeladapter.Input{Data: data, MimeType: mime}
		}
		name, err := b.uploadOne(ctx, in, fmt.Sprintf("mam-%s-%d", promptID[:8], i))
		if err != nil {
			return nil, err
		}
		files[i] = name
	}
	return files, nil
}

func (b *Backend) uploadOne(ctx context.Context, in *modeladapter.Input, base string) (string, error) {
	body, contentType, err := uploadBody(in, base+extension(in.MimeType))
	if err != nil {
		return "", err
	}
	data, err := b.Client.Request(ctx, http.MethodPost, "/upload/image", map[string]string{"Content-Type": contentType}, body, localhttp.MaxJSONBytes)
	if err != nil {
		return "", err
	}
	var out struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Name == "" {
		return "", b.Client.Empty("uploaded file name")
	}
	if out.Subfolder != "" {
		return out.Subfolder + "/" + out.Name, nil
	}
	return out.Name, nil
}

// noReferenceOK says a run without any reference image may leave the reference assignment empty: the workflow lists what it
// changes then (an img2img workflow samples from an empty latent).
func (b *Backend) noReferenceOK(v values, a localcfg.Assignment) bool {
	return a.Param == localcfg.ParamReferenceImage && len(v.references) == 0 && len(b.Model.Workflow.NoReference) > 0
}
