// Package traits is what the model filter (#1011) needs to know of a catalog model besides its license and its VRAM: how many parameters it
// has, whether it is uncensored, how fast it is, and what it takes as input (text, an image, a first frame, reference images).
//
// The data is split in two so that nothing is kept twice. What follows from the workflows (the inputs, whether a video comes with sound)
// is derived from the workflow templates (Facts, Derived), the same assignments the generation test reads its capabilities from, so a
// catalog entry cannot say T2V while its workflow takes a first frame. What no workflow says (the parameter count, uncensored, the
// speed and quality guides, the languages of a voice) is the catalog entry's own "traits" object (Traits).
//
// It is a package of its own because the catalog directory is full (linterly), and it imports nothing of the catalog so that the catalog
// can hold its type (like package platform).
package traits

import (
	"fmt"
	"regexp"
	"slices"
)

// Level is a guide of speed or of quality: a 目安 by the catalog author, not a measurement.
type Level string

// The levels. For Speed, Fast is a distilled or few-step model; for Quality, High is what the entry's own description says is the model to pick
// for the finish.
const (
	Fast     Level = "fast"
	Standard Level = "standard"
	Slow     Level = "slow"
	High     Level = "high"
	Basic    Level = "basic"
)

// speeds and qualities are the levels each guide takes.
var (
	speeds    = []Level{Fast, Standard, Slow}
	qualities = []Level{Basic, Standard, High}
)

// Valid reports whether l is a level of the guide: speed (true) or quality (false). The empty level (no guide) is valid.
func (l Level) Valid(speed bool) bool {
	if l == "" {
		return true
	}
	if speed {
		return slices.Contains(speeds, l)
	}
	return slices.Contains(qualities, l)
}

// Label is the short word of the badge ("" for no guide).
func (l Level) Label() string {
	switch l {
	case Fast:
		return "速い"
	case Standard:
		return "標準"
	case Slow:
		return "遅い"
	case High:
		return "高品質"
	case Basic:
		return "簡易"
	}
	return ""
}

// Traits are the fields of a catalog model's "traits" object. Every model has the object (the tests of the catalog fail otherwise): the
// fields that are not known stay empty.
type Traits struct {
	// ParamsB is the parameter count in billions of the model that makes the picture (0: not known). Only a figure the model's name, its description or its
	// vendor's card states is written; ParamsLabel is how it is shown ("14B", "2.6B（UNet）") and comes with it.
	ParamsB     float64 `json:"params_b,omitempty"`
	ParamsLabel string  `json:"params_label,omitempty"`
	// Uncensored: the weights or a part of the model (a LoRA, a text encoder, a merge) are made, tuned or released to produce adult content without the
	// refusals of the original. It is not the content rating: an adult model may be a general-purpose one that can also make adult images
	// (uncensored false), but an uncensored model is always an adult one (catalog.Validate).
	Uncensored bool `json:"uncensored"`
	// Speed and Quality are the optional guides of the screens' filters.
	Speed   Level `json:"speed,omitempty"`
	Quality Level `json:"quality,omitempty"`
	// Languages are the language codes (ISO 639-1: "ja", "en") a model that makes sound speaks; only for a model that has sound.
	Languages []string `json:"languages,omitempty"`
}

var (
	languagePattern = regexp.MustCompile(`^[a-z]{2}$`)
	// labelPattern is how a parameter count is shown: the number with B, and what it counts in brackets when the figure is not the whole model.
	labelPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?B(（[^）]+）)?$`)
)

// Validate checks the fields of one model's traits; audio is whether the model has sound (Derived.Audio).
func (t Traits) Validate(where string, audio bool) []error {
	var errs []error
	switch {
	case t.ParamsB < 0:
		errs = append(errs, fmt.Errorf("%s: traits.params_b must not be negative", where))
	case (t.ParamsB > 0) != (t.ParamsLabel != ""):
		errs = append(errs, fmt.Errorf("%s: traits.params_b and traits.params_label come together (both, or neither when the count is not known)", where))
	case t.ParamsLabel != "" && !labelPattern.MatchString(t.ParamsLabel):
		errs = append(errs, fmt.Errorf("%s: traits.params_label %q must be like \"14B\" or \"2.6B（UNet）\"", where, t.ParamsLabel))
	}
	if !t.Speed.Valid(true) {
		errs = append(errs, fmt.Errorf("%s: traits.speed must be fast, standard or slow", where))
	}
	if !t.Quality.Valid(false) {
		errs = append(errs, fmt.Errorf("%s: traits.quality must be basic, standard or high", where))
	}
	if len(t.Languages) > 0 && !audio {
		errs = append(errs, fmt.Errorf("%s: traits.languages belong to a model that makes sound", where))
	}
	for _, l := range t.Languages {
		if !languagePattern.MatchString(l) {
			errs = append(errs, fmt.Errorf("%s: traits.languages %q must be a two-letter ISO 639-1 code", where, l))
		}
	}
	return errs
}

// uncensoredMarker finds the names that say a file is (or comes from) an uncensored model: a file with one of them in its name or in the name of
// its repository makes the model an uncensored adult one (the rule that keeps a model from being listed as a general one while it carries such a file).
var uncensoredMarker = regexp.MustCompile(`(?i)uncensor|nsfw|heretic|abliterat`)

// UncensoredFile reports whether the name (a file name, or a URL with the repository) says that the file is uncensored.
func UncensoredFile(name string) bool { return uncensoredMarker.MatchString(name) }
