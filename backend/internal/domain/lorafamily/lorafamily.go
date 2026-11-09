// Package lorafamily is the one vocabulary of "which base model a LoRA is made for" (#1119): the families mam tells apart, the
// words that name them on the screens, and how a family is read from the metadata a LoRA file carries. It is a leaf (the standard
// library only) so that the workflow templates, the model manager and the generation test agree on it without importing each other.
//
// A family is a coarse answer: a LoRA of one family does not work with the base models of another, and within a family the
// versions (Pony, Illustrious, ...) are not told apart. What the metadata does not say is not guessed: FromMetadata returns nothing.
package lorafamily

import (
	"slices"
	"strings"
)

// The families. The keys are stored in state.json and in the workflow templates (lora_family): never rename one.
const (
	SDXL      = "sdxl"
	SD1       = "sd1"
	SD3       = "sd3"
	Flux      = "flux"
	Chroma    = "chroma"
	QwenImage = "qwen-image"
	Wan       = "wan"
)

// Option is a family with the word the screens show.
type Option struct{ Key, Label string }

// All lists the families in the order the screens show them.
func All() []Option {
	return []Option{
		{SDXL, "SDXL（Pony・Illustrious を含む）"}, {SD1, "SD 1.x"}, {SD3, "SD 3.x"}, {Flux, "FLUX.1"},
		{Chroma, "Chroma"}, {QwenImage, "Qwen-Image"}, {Wan, "Wan 2.x"},
	}
}

// Valid reports whether key is a family.
func Valid(key string) bool { return Label(key) != "" }

// Label is the word of the family ("" for a key that is none).
func Label(key string) string {
	for _, o := range All() {
		if o.Key == key {
			return o.Label
		}
	}
	return ""
}

// Labels are the words of the families, in the order of keys (a key that is none is left out).
func Labels(keys []string) []string {
	var out []string
	for _, k := range keys {
		if l := Label(k); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Normalize keeps the keys that are families, once each, in the order of All. A caller that takes keys from a form or a tool
// uses it, so that nothing else reaches the record.
func Normalize(keys []string) []string {
	var out []string
	for _, o := range All() {
		if slices.Contains(keys, o.Key) {
			out = append(out, o.Key)
		}
	}
	return out
}

// Mismatch reports that a LoRA made for the families lora does not fit a model of the family model: both are known and the
// model's is not among the LoRA's. A LoRA without a family, or a model without one, is not a mismatch (nothing is known).
func Mismatch(lora []string, model string) bool {
	return len(lora) > 0 && model != "" && !slices.Contains(lora, model)
}

// FromMetadata reads the families the __metadata__ of a LoRA file names: modelspec.architecture ("stable-diffusion-xl-v1-base/lora")
// and ss_base_model_version ("sdxl_base_v1-0"), the keys of the sd-scripts and the modelspec. Nothing else is read (a checkpoint
// file name or a tag says little), and a value mam does not know (FLUX.2, a new model) gives nothing: the user chooses.
func FromMetadata(meta map[string]string) []string {
	var keys []string
	for _, k := range []string{"modelspec.architecture", "ss_base_model_version"} {
		if f := fromText(strings.ToLower(meta[k])); f != "" {
			keys = append(keys, f)
		}
	}
	return Normalize(keys)
}

// fromText is the family a lower-cased architecture or base-model value names ("" when none).
func fromText(s string) string {
	has := func(words ...string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(s, w) })
	}
	switch {
	case has("flux.2", "flux2", "flux-2", "flux_2"):
		return "" // another generation of FLUX: its LoRAs are not FLUX.1's
	case has("stable-diffusion-xl", "sdxl"):
		return SDXL
	case has("stable-diffusion-v3", "stable-diffusion-3", "sd3", "sd_3"):
		return SD3
	case has("stable-diffusion-v1", "stable-diffusion-1", "sd_v1", "sd-v1", "sd1"):
		return SD1
	case has("chroma"):
		return Chroma
	case has("flux"):
		return Flux
	case has("qwen"):
		return QwenImage
	case has("wan2", "wan-2", "wan_2", "wan21", "wan22"):
		return Wan
	}
	return ""
}
