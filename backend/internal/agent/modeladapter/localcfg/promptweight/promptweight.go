// Package promptweight says whether a model reads the weights of a prompt: "(word:1.3)" and "(word)" (spec/architecture/local-models.md
// §4.4; ComfyUI does not read AUTOMATIC1111's "[word]"). It follows from the text encoder of the workflow and from how ComfyUI tokenizes for it, so it is a property of the
// workflow the way its reference mode is: ComfyUI cuts the weights out of the text and scales the embeddings of those words only for the
// tokenizers that do not disable weights (comfy/sd1_clip.py); the tokenizers of Qwen-Image, FLUX.2 klein, Hunyuan Video 1.5 and LTX-2.5
// pass disable_weights=True, so the brackets and the number reach the language model as plain text.
package promptweight

// Level is how far a model follows the weights of a prompt. Empty is "not said" and means Unknown.
type Level string

// The values of a workflow's prompt_weight.
const (
	// Supported: the weights are applied by CLIP encoders (SDXL, SD 3.5).
	Supported Level = "supported"
	// Weak: ComfyUI applies the weights, but what reads the sentence is a T5 (Flux.1, Chroma, Wan, LTX-Video 0.9.5), whose embeddings may
	// follow a weight poorly; the effect has not been checked on a GPU.
	Weak Level = "weak"
	// Unsupported: ComfyUI does not read the weights for the encoder: the text, brackets included, goes to the model as it is.
	Unsupported Level = "unsupported"
	// Unknown: not checked (a workflow of the user's own, or a model whose encoder is not known).
	Unknown Level = "unknown"
)

// Valid reports whether l is one of the four values (the empty one, "not said", is not).
func (l Level) Valid() bool {
	switch l {
	case Supported, Weak, Unsupported, Unknown:
		return true
	}
	return false
}

// OrUnknown is l, or Unknown when the workflow does not say.
func (l Level) OrUnknown() Level {
	if l == "" {
		return Unknown
	}
	return l
}
