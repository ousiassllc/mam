package modelfilter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// The query keys are the JSON keys of Condition, and a list is written as comma-separated words or as the key repeated (?kinds=image&kinds=video is
// ?kinds=image,video). A bool is "1" or "true" (anything else, or the key left out, is false). Unknown keys are ignored: the query of a screen
// carries other things (the page, the sort).
const (
	keyKinds         = "kinds"
	keyInputs        = "inputs"
	keyAdult         = "adult"
	keyUncensored    = "uncensored"
	keyParams        = "params"
	keyVRAMFit       = "vram_fit"
	keyCommercial    = "commercial"
	keyInstalled     = "installed"
	keyOrigins       = "origins"
	keyProviders     = "providers"
	keyAudio         = "audio"
	keyLanguages     = "languages"
	keySpeeds        = "speeds"
	keyQualities     = "qualities"
	keyAvailableOnly = "available_only"
	keyText          = "q"
)

// Keys lists the query keys of the condition; a form that posts the filter, and the handler that stores a screen's choice, use them.
func Keys() []string {
	return []string{keyKinds, keyInputs, keyAdult, keyUncensored, keyParams, keyVRAMFit, keyCommercial, keyInstalled, keyOrigins, keyProviders, keyAudio, keyLanguages, keySpeeds, keyQualities, keyAvailableOnly, keyText}
}

// words is a list of the values of key: the repeated key and the comma-separated words, trimmed, lower case, empty ones left out.
func words(v url.Values, key string) []string {
	var out []string
	for _, raw := range v[key] {
		for _, w := range strings.Split(raw, ",") {
			if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
				out = append(out, w)
			}
		}
	}
	return out
}

func flag(v url.Values, key string) bool {
	b, _ := strconv.ParseBool(strings.TrimSpace(v.Get(key)))
	return b
}

// ParseQuery reads a Condition from a query string (a screen's URL or form). The result is normalized (Normalize). A value that is none of the
// words of its key is an error that names the key; the screens show it, the MCP tools answer validation with it. A query without any key of the
// condition is the zero Condition (narrows nothing): use ParseQueryOr to start from a screen's default.
func ParseQuery(v url.Values) (Condition, error) {
	c := Condition{
		Adult: AdultFilter(strings.ToLower(strings.TrimSpace(v.Get(keyAdult)))), Uncensored: UncensoredFilter(strings.ToLower(strings.TrimSpace(v.Get(keyUncensored)))),
		Commercial: strings.Join(words(v, keyCommercial), ","), Installed: InstalledFilter(strings.ToLower(strings.TrimSpace(v.Get(keyInstalled)))),
		VRAMFit: flag(v, keyVRAMFit), Audio: flag(v, keyAudio), AvailableOnly: flag(v, keyAvailableOnly), Text: strings.TrimSpace(v.Get(keyText)),
		Providers: words(v, keyProviders), Languages: words(v, keyLanguages),
	}
	for _, w := range words(v, keyKinds) {
		c.Kinds = append(c.Kinds, Kind(w))
	}
	for _, w := range words(v, keyInputs) {
		c.Inputs = append(c.Inputs, traits.Input(w))
	}
	for _, w := range words(v, keyParams) {
		c.Params = append(c.Params, Band(w))
	}
	for _, w := range words(v, keyOrigins) {
		c.Origins = append(c.Origins, Origin(w))
	}
	for _, w := range words(v, keySpeeds) {
		c.Speeds = append(c.Speeds, traits.Level(w))
	}
	for _, w := range words(v, keyQualities) {
		c.Qualities = append(c.Qualities, traits.Level(w))
	}
	return c.finish()
}

// ParseQueryOr is ParseQuery for a screen that has a starting point: a query without any key of the condition (the screen was just opened, and
// nothing was remembered) gives start; a query with a key gives exactly what it says (so "?available_only=0" is the choice to show everything).
func ParseQueryOr(v url.Values, start Condition) (Condition, error) {
	for _, k := range Keys() {
		if _, ok := v[k]; ok {
			return ParseQuery(v)
		}
	}
	return start.Normalize(), nil
}

// Query writes the condition as a query string in the canonical form: only the parts that are set, in the order of Keys, values in their fixed order,
// available_only written as 1 or (when start is on and the condition's is off) 0, so that "show everything" survives a round trip. Pass the
// screen's starting point (Default for a picker, the zero Condition for the model manager). ParseQuery of the result gives the Normalize of c.
func (c Condition) Query(start Condition) url.Values {
	c = c.Normalize()
	v := url.Values{}
	set := func(key string, ws []string) {
		if len(ws) > 0 {
			v.Set(key, strings.Join(ws, ","))
		}
	}
	set(keyKinds, strs(c.Kinds))
	set(keyInputs, strs(c.Inputs))
	if c.Adult != "" {
		v.Set(keyAdult, string(c.Adult))
	}
	if c.Uncensored != "" {
		v.Set(keyUncensored, string(c.Uncensored))
	}
	set(keyParams, strs(c.Params))
	if c.VRAMFit {
		v.Set(keyVRAMFit, "1")
	}
	if c.Commercial != "" {
		v.Set(keyCommercial, c.Commercial)
	}
	if c.Installed != "" {
		v.Set(keyInstalled, string(c.Installed))
	}
	set(keyOrigins, strs(c.Origins))
	set(keyProviders, c.Providers)
	if c.Audio {
		v.Set(keyAudio, "1")
	}
	set(keyLanguages, c.Languages)
	set(keySpeeds, strs(c.Speeds))
	set(keyQualities, strs(c.Qualities))
	switch {
	case c.AvailableOnly:
		v.Set(keyAvailableOnly, "1")
	case start.AvailableOnly:
		v.Set(keyAvailableOnly, "0")
	}
	if c.Text != "" {
		v.Set(keyText, c.Text)
	}
	return v
}

func strs[T ~string](list []T) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = string(e)
	}
	return out
}

// ParseJSON reads a Condition from JSON (an MCP call, a choice a screen stored). Unknown keys are refused (a typo must not widen a search), and
// the result is normalized and validated like ParseQuery's.
func ParseJSON(data []byte) (Condition, error) {
	var c Condition
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Condition{}, fmt.Errorf("model filter: %w", err)
	}
	return c.finish()
}

// JSON writes the normalized condition; ParseJSON of it gives the condition back.
func (c Condition) JSON() ([]byte, error) { return json.Marshal(c.Normalize()) }

// finish normalizes the condition and checks its words.
func (c Condition) finish() (Condition, error) {
	c = c.Normalize()
	return c, c.Validate()
}

// Normalize is the canonical form of the condition: words lower-cased and trimmed, lists without duplicates and in the fixed order of the screens
// (words that are none of them stay at the end in the order given, for Validate to name), "all" of a three-way part and of the commercial part
// written as the empty word, the commercial list as the words of modellicense.Filter where one says it (an explicit "all", or all four answers, is "all": it
// does not follow a screen's or the setting's default, as the empty word does), the search text trimmed.
func (c Condition) Normalize() Condition {
	c.Kinds = sorted(lower(c.Kinds), Kinds())
	c.Inputs = sorted(lower(c.Inputs), traits.All())
	c.Params = sorted(lower(c.Params), Bands())
	c.Origins = sorted(lower(c.Origins), Origins())
	c.Providers = uniq(lower(c.Providers))
	c.Languages = uniq(lower(c.Languages))
	c.Speeds = sorted(lower(c.Speeds), []traits.Level{traits.Fast, traits.Standard, traits.Slow})
	c.Qualities = sorted(lower(c.Qualities), []traits.Level{traits.Basic, traits.Standard, traits.High})
	if c.Adult = AdultFilter(strings.ToLower(strings.TrimSpace(string(c.Adult)))); c.Adult == AdultAll {
		c.Adult = ""
	}
	if c.Uncensored = UncensoredFilter(strings.ToLower(strings.TrimSpace(string(c.Uncensored)))); c.Uncensored == UncensoredAll {
		c.Uncensored = ""
	}
	if c.Installed = InstalledFilter(strings.ToLower(strings.TrimSpace(string(c.Installed)))); c.Installed == InstalledAll {
		c.Installed = ""
	}
	old := c.Commercial
	if set, ok := parseCommercial(c.Commercial); ok {
		if c.Commercial = commercialWord(set); c.Commercial == "" && strings.TrimSpace(old) != "" {
			c.Commercial = string(modellicense.FilterAll) // an explicit "all" stays: unlike the empty word it does not follow the setting's default
		}
	} else {
		c.Commercial = strings.ToLower(strings.TrimSpace(c.Commercial))
	}
	c.Text = strings.TrimSpace(c.Text)
	return c
}

func lower[T ~string](list []T) []T {
	var out []T
	for _, e := range list {
		if w := T(strings.ToLower(strings.TrimSpace(string(e)))); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// uniq drops the repeats of a list and returns nil for an empty one.
func uniq[T comparable](list []T) []T {
	var out []T
	for _, e := range list {
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// sorted is the list without repeats, the known values first in the order of known and then the others in the order given.
func sorted[T comparable](list, known []T) []T {
	have := uniq(list)
	var out []T
	for _, k := range known {
		if slices.Contains(have, k) {
			out = append(out, k)
		}
	}
	for _, e := range have {
		if !slices.Contains(known, e) {
			out = append(out, e)
		}
	}
	return out
}
