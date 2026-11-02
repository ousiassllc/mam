package localcfg

import (
	"encoding/json"
	"fmt"
	"slices"
)

// ReferenceMode is how a workflow uses the reference images of an image generation.
type ReferenceMode string

const (
	// ModeEdit is an editing model (Qwen-Image 2.1): the reference images go to its text encoder, up to as many as the workflow
	// has reference_image slots, and the prompt says what to do with them (face only, style only, the picture to edit).
	ModeEdit ReferenceMode = "edit"
	// ModeImg2Img is a model that starts the sampling from one reference image: the denoise tells how far the result leaves it.
	ModeImg2Img ReferenceMode = "img2img"
)

// Override sets one input of one node of the graph: Value is a JSON literal or a link ["<node id>", <output index>].
type Override struct {
	Node  string          `json:"node"`
	Input string          `json:"input"`
	Value json.RawMessage `json:"value"`
}

// ReferenceCaps is what an image model does with reference images.
type ReferenceCaps struct {
	Mode ReferenceMode
	// Max is how many reference images one generation takes (0: none).
	Max int
	// Roles are the roles (modeladapter.Role*) a reference may have; an img2img model takes the whole picture only.
	Roles []string
}

// ReferenceCaps reports the reference images an image model takes, from the assignments of its workflow. The zero value is
// returned for a model that takes none.
func (m Model) ReferenceCaps() ReferenceCaps {
	w := m.Workflow
	if m.Media != MediaImage || w == nil || !w.Has(ParamReferenceImage) {
		return ReferenceCaps{}
	}
	caps := ReferenceCaps{Mode: w.ReferenceMode, Max: w.Count(ParamReferenceImage), Roles: []string{"reference"}}
	if caps.Mode == ModeEdit {
		caps.Roles = []string{"reference", "face", "style", "edit"}
	}
	return caps
}

// SupportsLoRA reports whether a LoRA of a request can be put into the workflow.
func (m Model) SupportsLoRA() bool { return m.Workflow != nil && m.Workflow.LoraModelNode != "" }

// validateReferenceMode checks ReferenceMode, NoReference and LoraModelNode against the graph.
func validateReferenceMode(where string, w Workflow, g Graph) []error {
	var errs []error
	switch w.ReferenceMode {
	case "", ModeEdit, ModeImg2Img:
	default:
		errs = append(errs, fmt.Errorf("%s: reference_mode must be edit or img2img", where))
	}
	if w.ReferenceMode != "" && !w.Has(ParamReferenceImage) {
		errs = append(errs, fmt.Errorf("%s: reference_mode needs a reference_image assignment", where))
	}
	if w.ReferenceMode == ModeImg2Img && w.Count(ParamReferenceImage) != 1 {
		errs = append(errs, fmt.Errorf("%s: an img2img workflow takes exactly one reference image", where))
	}
	if w.ReferenceDenoiseMin < 0 || w.ReferenceDenoiseMin >= 1 {
		errs = append(errs, fmt.Errorf("%s: reference_denoise_min must be within 0 and below 1", where))
	}
	for i, o := range w.NoReference {
		n, ok := g[o.Node]
		if !ok || n.Inputs[o.Input] == nil || len(o.Value) == 0 || !json.Valid(o.Value) {
			errs = append(errs, fmt.Errorf("%s: no_reference[%d] must name an input of a node of the workflow and a JSON value", where, i))
		}
	}
	if w.LoraModelNode != "" {
		if _, ok := g[w.LoraModelNode]; !ok {
			errs = append(errs, fmt.Errorf("%s: lora_model_node %q is not a node of the workflow", where, w.LoraModelNode))
		}
	}
	return errs
}

// The roles of a reference image of an image model: what the model takes from the picture.
const (
	RoleWhole = "reference" // the whole picture: its look, people and objects (the default)
	RoleFace  = "face"      // the face (identity) only: hair, clothes and pose follow the prompt
	RoleStyle = "style"     // the painting style and mood only: the content follows the prompt
	RoleEdit  = "edit"      // the picture the prompt edits
)

// DefaultReferenceStrength is the strength of a reference that names none: strong enough to show, weak enough to leave the
// prompt its say. DefaultLoRAStrength suits a face LoRA used together with a reference image.
const (
	DefaultReferenceStrength = 0.7
	DefaultLoRAStrength      = 0.4
)

// AvoidPoseNegative is added to the negative prompt when the user asks that a reference's pose and composition not be copied.
const AvoidPoseNegative = "same pose as reference, same composition as reference, studio portrait"

// LoRA is a LoRA file of the engine (its name in the loras folder) and the strength it is applied with.
type LoRA struct {
	Name     string
	Strength float64
}

// RefOption is what a request says about one reference image: its role (RoleWhole when empty) and its strength, how strongly it
// shows in the result (0..1; 0 is DefaultReferenceStrength).
type RefOption struct {
	Role     string
	Strength float64
}

// StrengthOrDefault is the strength to use: 0 (not given) is the default, anything else is kept within 0..1.
func (o RefOption) StrengthOrDefault() float64 {
	switch {
	case o.Strength <= 0:
		return DefaultReferenceStrength
	case o.Strength > 1:
		return 1
	}
	return o.Strength
}

// ImageOptions are the parts of an image request that only a workflow with reference images or a LoRA uses: Refs is one entry
// for each input image of the request, in its order (a missing entry is the whole picture at the default strength).
type ImageOptions struct {
	Refs []RefOption
	LoRA *LoRA
}

// Ref is the option of the i-th input image.
func (o ImageOptions) Ref(i int) RefOption {
	if i < 0 || i >= len(o.Refs) {
		return RefOption{}
	}
	return o.Refs[i]
}

// validateReferences checks the reference image assignments: the indexes are 0..n-1 without gaps or
// repeats, and a last frame needs a first frame.
func validateReferences(where string, media Media, w Workflow) []error {
	var errs []error
	n := w.Count(ParamReferenceImage)
	for i := range n {
		if !slices.ContainsFunc(w.Assignments, func(a Assignment) bool { return a.Param == ParamReferenceImage && a.Index == i }) {
			errs = append(errs, fmt.Errorf("%s: reference_image indexes must be 0..%d without gaps or repeats", where, n-1))
			break
		}
	}
	if media == MediaVideo && w.Has(ParamLastFrame) && !w.Has(ParamFirstFrame) {
		errs = append(errs, fmt.Errorf("%s: a last_frame assignment needs a first_frame assignment", where))
	}
	return errs
}
