// Package record is the shape of what the generation test keeps of a batch and of a result (the JSON columns of the history's
// tables and the payload of its job) and the views of it that the screens and the MCP tools show. It is a package of its own so
// that the packages that write the history (the batch runner) and the ones that read it (package history, package gentest) share
// one type without importing each other.
package record

import (
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
	// SpeedMode is the speed mode the video model was asked for (absent: its default).
	SpeedMode string `json:"speed_mode,omitempty"`
	// Sampling is the steps, cfg, sampler and scheduler the model ran with (the request's value where it took one, else the model's
	// default; not the steps and cfg of a speed mode). Absent: the model takes none, or the result is older than the setting.
	Sampling *sampling.Settings `json:"sampling,omitempty"`
	// Inpaint is set when the result redrew part of a picture. Absent for every other result.
	Inpaint *Inpaint `json:"inpaint,omitempty"`
	// Upscale is the enlargement the result is (a result of "make it larger"): the source and the values (Upscale). Absent: a generated result.
	Upscale *Upscale `json:"upscale,omitempty"`
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
