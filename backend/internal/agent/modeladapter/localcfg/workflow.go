package localcfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/promptweight"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
)

// Param is a value of a generation that an Assignment puts into a node of a ComfyUI workflow.
type Param string

// The values mam can put into a workflow.
const (
	ParamPrompt         Param = "prompt"
	ParamNegativePrompt Param = "negative_prompt"
	ParamSeed           Param = "seed"
	ParamWidth          Param = "width"
	ParamHeight         Param = "height"
	// ParamFrames is the number of frames of a video (the duration times the frame rate).
	ParamFrames Param = "frames"
	ParamFPS    Param = "fps"
	// ParamReferenceImage is a reference image (Assignment.Index picks which one, from 0): the file name
	// of the image uploaded to the server.
	ParamReferenceImage Param = "reference_image"
	ParamFirstFrame     Param = "first_frame"
	ParamLastFrame      Param = "last_frame"
	// ParamReferenceDenoise is how far an img2img workflow leaves its reference image: 1 minus the strength of the reference
	// (1 when the run has no reference). It is image-only and goes into a float input (a sampler's denoise).
	ParamReferenceDenoise Param = "reference_denoise"
)

// imageParams are the values an image workflow takes; videoParams the ones a video workflow takes.
var (
	imageParams = []Param{ParamPrompt, ParamNegativePrompt, ParamSeed, ParamWidth, ParamHeight, ParamReferenceImage, ParamReferenceDenoise, ParamReferenceStrength, ParamSteps, ParamCFG, ParamSampler, ParamScheduler}
	videoParams = []Param{ParamPrompt, ParamNegativePrompt, ParamSeed, ParamWidth, ParamHeight, ParamFrames, ParamFPS, ParamReferenceImage, ParamFirstFrame, ParamLastFrame, ParamSteps, ParamCFG, ParamSampler, ParamScheduler}
)

// ImageParams lists the values an image workflow takes.
func ImageParams() []Param { return slices.Clone(imageParams) }

// VideoParams lists the values a video workflow takes.
func VideoParams() []Param { return slices.Clone(videoParams) }

// Assignment says where a value goes: the input Input of the node Node (an ID of the workflow).
type Assignment struct {
	Param Param  `json:"param"`
	Node  string `json:"node"`
	Input string `json:"input"`
	// Index is which reference image a ParamReferenceImage assignment takes (0 is the first).
	Index int `json:"index,omitempty"`
	// Optional (ParamReferenceImage only) lets the workflow run without that reference: the assignment's node is dropped and the
	// inputs of other nodes that linked to it are removed (a variable number of references).
	Optional bool `json:"optional,omitempty"`
	// Ratio (ParamSteps only, 0 < Ratio < 1) puts round(steps * Ratio) there instead of the steps: where a two-stage sampler (Wan 2.2) hands over.
	Ratio float64 `json:"ratio,omitempty"`
}

// Workflow is a ComfyUI workflow in API format with the table that tells mam where its values go.
type Workflow struct {
	// Graph is the workflow as "Save (API Format)" writes it: {"<node id>": {"class_type", "inputs"}}.
	Graph json.RawMessage `json:"graph"`
	// Assignments are where the values of a generation go.
	Assignments []Assignment `json:"assignments"`
	// OutputNode is the node whose result is taken: a SaveImage, SaveVideo or similar node.
	OutputNode string `json:"output_node"`
	// FPS is the frame rate of a video workflow (0: 16). FramesStep, when positive, makes the frame count
	// n*FramesStep+1 (Wan 2.x takes 4n+1, LTX 8n+1); 0 uses the plain duration times FPS.
	FPS        int `json:"fps,omitempty"`
	FramesStep int `json:"frames_step,omitempty"`
	// SizeMultiple is the multiple the width and height of a video are rounded to (0: 16).
	SizeMultiple int `json:"size_multiple,omitempty"`
	// ReferenceMode says how the workflow uses reference images (ReferenceMode); empty when it uses none or does not say.
	ReferenceMode ReferenceMode `json:"reference_mode,omitempty"`
	// NoReference is what a run without any reference image changes in the graph (an img2img workflow samples from an empty
	// latent then).
	NoReference []Override `json:"no_reference,omitempty"`
	// ReferenceDenoiseMin (img2img, 0: no floor) is the lowest denoise a reference of strength 1 runs with. The denoise of a
	// strength s is 1 - (1-min)*s: a model whose img2img changes nothing below some denoise (Chroma's schedule is crowded at
	// the low end) gets the strength slider spread over the range in which the picture does change.
	ReferenceDenoiseMin float64 `json:"reference_denoise_min,omitempty"`
	// LoraModelNode is the node whose output 0 is the model: a LoRA of a request is inserted behind it. Empty: no LoRA.
	LoraModelNode string `json:"lora_model_node,omitempty"`
	// PromptWeight says whether the model reads the weights of a prompt, "(word:1.3)" (package promptweight); empty: not said (unknown).
	PromptWeight promptweight.Level `json:"prompt_weight,omitempty"`
	// SpeedModes are the ways a workflow can sample (SpeedMode); the first is the graph as it stands and the default. Empty: one way.
	SpeedModes []SpeedMode `json:"speed_modes,omitempty"`
	Upscale    *Upscale    `json:"upscale,omitempty"` // marks a workflow that enlarges a picture; nil: one that makes pictures
	// RequiresNodes are the IDs of the custom nodes (the allowlist, modelmgr/nodecat) the graph uses besides the core nodes. Empty: core only.
	RequiresNodes []string `json:"requires_nodes,omitempty"`
	// QualityPresets are the steps and cfg of the fast and the high quality of the screen's "品質" buttons (Issue #977); nil: the model has no quality to choose.
	QualityPresets *sampling.Presets `json:"quality_presets,omitempty"`
	// ExposedInputs are the inputs of the graph the screen offers as fields of their own (Issue #977; package exposed); empty: none.
	ExposedInputs []exposed.Input `json:"exposed_inputs,omitempty"`
}

// DefaultFPS is the frame rate of a video workflow that names none.
const DefaultFPS = 16

// FPSOrDefault is FPS with the default applied.
func (w Workflow) FPSOrDefault() int {
	if w.FPS <= 0 {
		return DefaultFPS
	}
	return w.FPS
}

// Frames is the frame count of a clip of the given length.
func (w Workflow) Frames(seconds float64) int {
	n := seconds * float64(w.FPSOrDefault())
	if w.FramesStep > 0 {
		return int(n/float64(w.FramesStep)+0.5)*w.FramesStep + 1
	}
	return max(1, int(n+0.5))
}

// Has reports whether the workflow has an assignment of p.
func (w Workflow) Has(p Param) bool {
	return slices.ContainsFunc(w.Assignments, func(a Assignment) bool { return a.Param == p })
}

// Count is the number of assignments of p.
func (w Workflow) Count(p Param) int {
	n := 0
	for _, a := range w.Assignments {
		if a.Param == p {
			n++
		}
	}
	return n
}

// Node is one node of a workflow graph.
type Node struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
}

// Graph is a workflow in API format.
type Graph map[string]Node

// maxGraphBytes bounds an imported workflow.
const maxGraphBytes = 2 << 20

// Normalize returns the API-format graph in raw, which may be the graph itself or the {"prompt": graph}
// object the server's POST /prompt takes. A workflow saved in the editor's own format (with "nodes" and
// "links") is refused with a hint: only the API format can be run.
func Normalize(raw []byte) (json.RawMessage, error) {
	if len(raw) > maxGraphBytes {
		return nil, errors.New("the workflow is too large")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("the workflow is not a JSON object: %w", err)
	}
	if _, ok := top["nodes"]; ok {
		return nil, errors.New("this is the editor's format: save the workflow with \"Save (API Format)\" in ComfyUI")
	}
	if inner, ok := top["prompt"]; ok && len(top) <= 3 {
		raw = inner
	}
	if _, err := ParseGraph(raw); err != nil {
		return nil, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

// ParseGraph decodes and checks a graph: it has nodes, each node has a class_type and inputs, and every
// link ([node id, output index]) names a node of the graph.
func ParseGraph(raw []byte) (Graph, error) {
	var g Graph
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("the workflow graph cannot be read (API format expected): %w", err)
	}
	if len(g) == 0 {
		return nil, errors.New("the workflow graph has no nodes")
	}
	for _, id := range sortedKeys(g) {
		n := g[id]
		if n.ClassType == "" {
			return nil, fmt.Errorf("node %s has no class_type (API format expected)", id)
		}
		for _, in := range sortedKeys(n.Inputs) {
			if target, ok := linkTarget(n.Inputs[in]); ok {
				if _, exists := g[target]; !exists {
					return nil, fmt.Errorf("node %s input %s links to the missing node %s", id, in, target)
				}
			}
		}
	}
	return g, nil
}

// linkTarget is the node a link value ["<id>", <index>] points to.
func linkTarget(v json.RawMessage) (string, bool) {
	var arr []json.RawMessage
	if json.Unmarshal(v, &arr) != nil || len(arr) != 2 {
		return "", false
	}
	var id string
	var idx int
	if json.Unmarshal(arr[0], &id) != nil || json.Unmarshal(arr[1], &idx) != nil {
		return "", false
	}
	return id, true
}

// IsLink reports whether an input value is a link to another node's output.
func IsLink(v json.RawMessage) bool { _, ok := linkTarget(v); return ok }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// validateWorkflow checks a workflow for media: the graph, the assignments (a known value, an existing
// node and a literal input to put it in, no input fed twice), the output node and the frame settings.
func validateWorkflow(where string, media Media, w Workflow) []error {
	g, err := ParseGraph(w.Graph)
	if err != nil {
		return []error{fmt.Errorf("%s: workflow: %w", where, err)}
	}
	var errs []error
	allowed := imageParams
	if media == MediaVideo {
		allowed = videoParams
	}
	seen := map[string]bool{}
	for i, a := range w.Assignments {
		at := fmt.Sprintf("%s: assignments[%d]", where, i)
		node, ok := g[a.Node]
		switch {
		case !slices.Contains(allowed, a.Param):
			errs = append(errs, fmt.Errorf("%s: %q is not a value of a %s workflow", at, a.Param, media))
		case !ok:
			errs = append(errs, fmt.Errorf("%s: the workflow has no node %q", at, a.Node))
		case node.Inputs[a.Input] == nil:
			errs = append(errs, fmt.Errorf("%s: node %s (%s) has no input %q", at, a.Node, node.ClassType, a.Input))
		case IsLink(node.Inputs[a.Input]):
			errs = append(errs, fmt.Errorf("%s: input %q of node %s is fed by another node, a value cannot be put there", at, a.Input, a.Node))
		case seen[a.Node+"\x00"+a.Input]:
			errs = append(errs, fmt.Errorf("%s: input %q of node %s is assigned twice", at, a.Input, a.Node))
		}
		seen[a.Node+"\x00"+a.Input] = true
		if a.Index < 0 || (a.Index > 0 && a.Param != ParamReferenceImage) {
			errs = append(errs, fmt.Errorf("%s: index applies to reference_image only and cannot be negative", at))
		}
		if a.Optional && a.Param != ParamReferenceImage {
			errs = append(errs, fmt.Errorf("%s: optional applies to reference_image only", at))
		}
	}
	if !w.Has(ParamPrompt) && w.Upscale == nil { // an upscale workflow without sampling has no prompt (validateUpscale)
		errs = append(errs, fmt.Errorf("%s: no assignment of the prompt", where))
	}
	if _, ok := g[w.OutputNode]; !ok {
		errs = append(errs, fmt.Errorf("%s: output_node %q is not a node of the workflow", where, w.OutputNode))
	}
	errs = append(errs, validateReferences(where, media, w)...)
	errs = append(errs, validateReferenceMode(where, w, g)...)
	errs = append(errs, validateSpeedModes(where, w, g)...)
	errs = append(errs, validateRequiresNodes(where, w)...)
	errs = append(errs, validateSampling(where, w, g)...)
	errs = append(errs, validateExposed(where, w, g)...)
	errs = append(errs, append(validatePromptWeight(where, w), validateUpscale(where, media, w, g)...)...)
	if w.FPS < 0 || w.FPS > 240 || w.FramesStep < 0 || w.FramesStep > 64 || w.SizeMultiple < 0 || w.SizeMultiple > 128 {
		errs = append(errs, fmt.Errorf("%s: fps must be within 0-240, frames_step within 0-64 and size_multiple within 0-128", where))
	}
	return errs
}
