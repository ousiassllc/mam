// Package record is the shape of what the generation test keeps of a batch and of a result (the JSON columns of the history's
// tables and the payload of its job) and the views of it that the screens and the MCP tools show. It is a package of its own so
// that the packages that write the history (the batch runner) and the ones that read it (package history, package gentest) share
// one type without importing each other.
package record

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
)

// Payload is the payload of a gentest job: the batch it runs.
type Payload struct {
	BatchID string `json:"batch_id"`
}

// BatchParams is gentest_batch.params.
type BatchParams struct {
	AspectRatio        string  `json:"aspect_ratio"`
	Resolution         string  `json:"resolution,omitempty"`
	DurationSeconds    float64 `json:"duration_seconds,omitempty"`
	Count              int     `json:"count"`
	TranslateToEnglish bool    `json:"translate_to_english,omitempty"`
}

// ResultParams is gentest_result.params: what was asked of that model.
type ResultParams struct {
	AspectRatio     string  `json:"aspect_ratio"`
	Resolution      string  `json:"resolution,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	// Translation is the English the prompt (and negative prompt) was translated to; nil when it was sent as typed.
	Translation *SentText `json:"translation,omitempty"`
	// AvoidPose says the negative prompt got refs.WithAvoidPose; LoRA is the LoRA the model was given (image models only).
	AvoidPose bool       `json:"avoid_pose,omitempty"`
	LoRA      *refs.LoRA `json:"lora,omitempty"`
	// SpeedMode is the speed mode the model was asked for (absent: its default).
	SpeedMode string `json:"speed_mode,omitempty"`
	// Sampling is the steps, cfg, sampler and scheduler the model ran with (the request's value where it took one, else the model's
	// default; not the steps and cfg of a speed mode). Absent: the model takes none, or the result is older than the setting.
	Sampling *sampling.Settings `json:"sampling,omitempty"`
	// Exposed are the values the model's exposed inputs ran with (by key "<node>.<input>"; the request's value, else the graph's own). Absent: it has none.
	Exposed map[string]any `json:"exposed,omitempty"`
	// GPT are the options the result was asked for (GPT Image 2.6: complexity, GIF / SVG output). Absent: none was (a PNG at OpenAI's default complexity).
	GPT *gptimage.Options `json:"gpt_image,omitempty"`
	// Inpaint is set when the result redrew part of a picture. Absent for every other result.
	Inpaint *Inpaint `json:"inpaint,omitempty"`
	// Adult says the model that made the result is an adult model of the model manager's catalog, as it was when the result was made (so that deleting the model
	// later does not lose it). Sending the result to an external cloud asks the user first. Absent: not adult, or the result is older than this field.
	Adult bool `json:"adult,omitempty"`
	// Upscale is the enlargement the result is (a result of "make it larger"): the source and the values (Upscale). Absent: a generated result.
	Upscale *Upscale `json:"upscale,omitempty"`
	// VideoEnhance is the enlargement or the smoothing of a video the result is (a result of "make a video larger / smoother"): the source and the values. Absent: a generated result.
	VideoEnhance *VideoEnhance `json:"video_enhance,omitempty"`
	// FaceSwap is set for a result of "swap the face" (FaceFusion, an external tool): the picture it swapped into and the models. Absent: a generated result.
	FaceSwap *FaceSwap `json:"face_swap,omitempty"`
	// FaceSimilarity is how much the face of the result is the face of the batch's reference of the role "face" (measured when it was made).
	FaceSimilarity *FaceSimilarity `json:"face_similarity,omitempty"`
	// UnusedInputs are the input images of the batch this model was not given, with why (#1009). Absent: it used every one (or the batch had none).
	UnusedInputs []UnusedInput `json:"unused_inputs,omitempty"`
}

// UnusedInput is an input image of the batch that the model of a result was not given: the input (its id in the detail's inputs), its role and
// the reason in words.
type UnusedInput struct {
	InputID string `json:"input_id" jsonschema:"使われなかった入力画像（get_generation_result の inputs の input_id）"`
	Role    string `json:"role" jsonschema:"その画像の役割（reference / face / style / edit / pose / first_frame / last_frame）"`
	Reason  string `json:"reason" jsonschema:"使われなかった理由（日本語）"`
}

// Inpaint is a partial redraw: the strength that was sent (nil: the template's own), where the source picture came from, and the
// id of the mask input of the batch.
type Inpaint struct {
	Denoise    *float64 `json:"denoise,omitempty"`
	SourceKind string   `json:"source_kind,omitempty"`
	SourceID   string   `json:"source_id,omitempty"`
	MaskInput  string   `json:"mask_input,omitempty"`
}

// SentText is the English a result's prompt and negative prompt were translated to (gentest_result.params "translation").
type SentText struct {
	Prompt   string `json:"prompt"`
	Negative string `json:"negative_prompt,omitempty"`
	// LostWeights lists the weights of the prompt syntax ("(word:1.3)", brackets) that the English does not have any more (found by
	// prompttranslate.LostWeights); absent when every one survived.
	LostWeights []string `json:"lost_weights,omitempty"`
}
