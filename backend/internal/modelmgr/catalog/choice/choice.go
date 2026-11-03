// Package choice describes the models a local runtime can run with (Irodori-TTS: v4.1-Small, v4-Large, ...): the runtime's program is one,
// and its weights differ by the model the user chooses (spec/architecture/model-manager/local-runtime.md §14). The catalog embeds the type in a
// runtime's weights (catalog.ResourceSet.Choices; the assets of a choice name it by catalog.Resource.Choice) and calls Check from its own
// validation. It is a package of its own, like parts and platform, because the catalog directory is at its line budget.
package choice

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The refusals of the choice of a model (service/localruntime/choices): the license of a model has to be accepted before it is chosen,
// fetched or started, and there is no such model.
var (
	ErrConsentRequired = errors.New("choice: the license of the model has to be accepted")
	ErrUnknown         = errors.New("choice: no such model")
)

// Consent is what the user has to accept before a model is chosen (the Gemma Terms of Use of the Large models). It rides on the consent
// record that the model manager already has (store.Consent: the version that was shown and when): a new Version asks again.
type Consent struct {
	// Version identifies the text; Text is what the screen shows next to the checkbox.
	Version string `json:"version"`
	Text    string `json:"text"`
}

// Choice is one model of a runtime.
type Choice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Default marks the model that is used until the user chooses another (exactly one choice has it).
	Default bool `json:"default,omitempty"`
	// Kind and Languages are what the filter of the model list ("音声", "日本語") reads (spec: Issue #1011): the kind of what the model makes
	// ("tts") and the languages it speaks (ISO 639-1 codes).
	Kind      string   `json:"kind"`
	Languages []string `json:"languages"`
	// License is the id of a license of the runtime's weights (catalog.ResourceSet.Licenses) that applies to the model.
	License string `json:"license"`
	// Consent is set for a model whose license has to be accepted before it is chosen (nil for none).
	Consent *Consent `json:"consent,omitempty"`
	// Cautions are the restrictions of use the screen shows with the model (the ethical restrictions of the model card).
	Cautions []string `json:"cautions"`
	// VRAMMinGB and VRAMRecommendedGB are the video memory the model needs (0 recommended: the minimum is also the recommendation) and
	// VRAMLimits says what a GPU between the two gives up; VRAMNote says where the figure comes from, and VRAMEstimate that it is mam's guess.
	VRAMMinGB         int    `json:"vram_min_gb"`
	VRAMRecommendedGB int    `json:"vram_recommended_gb,omitempty"`
	VRAMLimits        string `json:"vram_limits,omitempty"`
	VRAMNote          string `json:"vram_note"`
	VRAMEstimate      bool   `json:"vram_estimate"`
	// Env is added to the environment of the server when this model is chosen ("NAME=value").
	Env []string `json:"env,omitempty"`
}

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,30}$`)
	envPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*=.*$`)
	langRegexp = regexp.MustCompile(`^[a-z]{2,3}$`)
)

// Default is the choice that is used until another is chosen ("" for a runtime without choices).
func Default(cs []Choice) string {
	if i := slices.IndexFunc(cs, func(c Choice) bool { return c.Default }); i >= 0 {
		return cs[i].ID
	}
	return ""
}

// Check validates the choices of the runtime named where: licenses are the ids of the runtime's weights licenses and used the choices the
// assets name (every choice has at least one asset, and every asset names a choice that exists).
func Check(where string, cs []Choice, licenses map[string]bool, used map[string]bool) []error {
	var errs []error
	seen, defaults := map[string]bool{}, 0
	for _, c := range cs {
		errs = append(errs, c.check(where+" choice "+c.ID, seen[c.ID], licenses, used)...)
		seen[c.ID] = true
		if c.Default {
			defaults++
		}
	}
	if len(cs) > 0 && defaults != 1 {
		errs = append(errs, fmt.Errorf("%s: exactly one choice is the default", where))
	}
	for id := range used {
		if id != "" && !seen[id] {
			errs = append(errs, fmt.Errorf("%s: an asset belongs to the choice %q, which does not exist", where, id))
		}
	}
	return errs
}

// check is the first problem of one choice (and every malformed env line); duplicate says an earlier choice had the same id.
func (c Choice) check(cw string, duplicate bool, licenses, used map[string]bool) []error {
	var errs []error
	switch {
	case !idPattern.MatchString(c.ID) || duplicate:
		errs = append(errs, fmt.Errorf("%s: the id must be a slug that is used once", cw))
	case c.Name == "" || strings.TrimSpace(c.Description) == "" || c.Kind == "":
		errs = append(errs, fmt.Errorf("%s: name, description and kind are required", cw))
	case len(c.Languages) == 0 || slices.ContainsFunc(c.Languages, func(l string) bool { return !langRegexp.MatchString(l) }):
		errs = append(errs, fmt.Errorf("%s: languages are required (ISO 639 codes)", cw))
	case !licenses[c.License]:
		errs = append(errs, fmt.Errorf("%s: license %q is not one of the licenses of the set", cw, c.License))
	case len(c.Cautions) == 0:
		errs = append(errs, fmt.Errorf("%s: the restrictions of use (cautions) are required", cw))
	case c.VRAMMinGB <= 0 || c.VRAMRecommendedGB < 0 || strings.TrimSpace(c.VRAMNote) == "":
		errs = append(errs, fmt.Errorf("%s: vram needs a minimum and the source of the figure", cw))
	case c.VRAMRecommendedGB > c.VRAMMinGB && strings.TrimSpace(c.VRAMLimits) == "":
		errs = append(errs, fmt.Errorf("%s: a recommendation above the minimum needs vram_limits (what a smaller GPU gives up)", cw))
	case c.Consent != nil && (c.Consent.Version == "" || strings.TrimSpace(c.Consent.Text) == ""):
		errs = append(errs, fmt.Errorf("%s: consent needs a version and its text", cw))
	case !used[c.ID]:
		errs = append(errs, fmt.Errorf("%s: no asset belongs to the choice", cw))
	}
	for _, e := range c.Env {
		if !envPattern.MatchString(e) {
			errs = append(errs, fmt.Errorf("%s: env %q must be NAME=value", cw, e))
		}
	}
	return errs
}
