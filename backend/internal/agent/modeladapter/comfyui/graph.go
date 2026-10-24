package comfyui

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/aspectsize"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
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
}

func imageValues(req modeladapter.ImageRequest) values {
	w, h := aspectsize.Flux(req.AspectRatio)
	return values{prompt: req.Prompt, negative: defaultNegative, seed: newSeed(), width: w, height: h, references: req.Inputs}
}

func videoValues(wf localcfg.Workflow, req modeladapter.VideoRequest) values {
	w, h := localcfg.VideoSize(req.Resolution, req.AspectRatio, wf.SizeMultiple)
	return values{
		prompt: req.Prompt, negative: req.NegativePrompt, seed: newSeed(), width: w, height: h,
		frames: wf.Frames(req.DurationSeconds), fps: wf.FPSOrDefault(),
		references: req.References, first: req.FirstFrame, last: req.LastFrame,
	}
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
	}
	return nil
}

// missing names the first image an assignment of the workflow needs and the request does not have: the
// workflow cannot run without it (an assignment is a required input), so the request is refused at once and
// the Adapter routes to the alternative model.
func (b *Backend) missing(v values) error {
	for _, a := range b.Model.Workflow.Assignments {
		if in, isImage := v.input(a); isImage && in == nil {
			return b.Client.Error(http.StatusUnprocessableEntity, fmt.Sprintf("the workflow needs %s (%s), the request has none", a.Param, a.Node), nil)
		}
	}
	return nil
}

// build puts the values into a copy of the workflow's graph. files are the names the images were uploaded
// as, by assignment index.
func (b *Backend) build(v values, files map[int]string) (json.RawMessage, error) {
	g, err := localcfg.ParseGraph(b.Model.Workflow.Graph)
	if err != nil {
		return nil, b.Client.Error(http.StatusUnprocessableEntity, "the workflow is unusable: "+err.Error(), nil)
	}
	for i, a := range b.Model.Workflow.Assignments {
		value := v.scalar(a.Param)
		if name, ok := files[i]; ok {
			value = name
		}
		raw, _ := json.Marshal(value)
		g[a.Node].Inputs[a.Input] = raw
	}
	return json.Marshal(g)
}

// upload sends every image the workflow takes to the server (POST /upload/image keeps any bytes under a
// name with the extension it was given) and returns the names to put into the graph, by assignment index.
func (b *Backend) upload(ctx context.Context, v values, promptID string) (map[int]string, error) {
	files := map[int]string{}
	for i, a := range b.Model.Workflow.Assignments {
		in, isImage := v.input(a)
		if !isImage {
			continue
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
