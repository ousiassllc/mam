package modelfilter

import (
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// VRAMFitGB is the GPU memory of the condition "VRAM 16GB で動く" (Condition.VRAMFit).
const VRAMFitGB = 16

// Item is a model reduced to what the conditions look at. Each screen and tool builds its Items from its own model type; the zero value of a
// field is "not known / none".
type Item struct {
	ID    string
	Name  string
	Kinds []Kind
	// Inputs are the inputs the model takes (traits.Derived.Inputs for a catalog model, modelcatalog.Info.Inputs for a generation test model).
	Inputs []traits.Input
	// Rating: Adult is the content rating, Uncensored the traits flag (an uncensored model is an adult one).
	Adult, Uncensored bool
	// ParamsB is the parameter count in billions (0: not known).
	ParamsB float64
	// VRAMMinGB is the minimum of the VRAM guideline (0: the model has none: a cloud model, an LLM).
	VRAMMinGB  int
	Commercial modellicense.Commercial
	// Installed is false for a local model that is not on this PC; a model that has nothing to install (a cloud model) is true.
	Installed bool
	Origin    Origin
	Provider  string
	// Audio is true for a video model that makes sound.
	Audio          bool
	Speed, Quality traits.Level
	// Tags are the words the text search reads besides the name (the tags, the trigger words, the vendor).
	Tags []string
	// Available is false when the model cannot be chosen now; Reason says why (the same words as the screens and the MCP tools).
	Available bool
	Reason    string
}

// Match reports whether the item passes every part of the condition that is set.
func (c Condition) Match(it Item) bool {
	return (!c.AvailableOnly || it.Available) && c.matchesKind(it) && c.matchesRating(it) && c.matchesWhere(it) && c.matchesGuides(it) && c.matchesText(it)
}

// matchesKind: what the model makes and takes.
func (c Condition) matchesKind(it Item) bool {
	if len(c.Kinds) > 0 && !slices.ContainsFunc(it.Kinds, func(k Kind) bool { return slices.Contains(c.Kinds, k) }) {
		return false
	}
	return hasAll(it.Inputs, c.Inputs) && (!c.Audio || it.Audio)
}

// matchesRating: the content rating and the uncensored flag, each three-way.
func (c Condition) matchesRating(it Item) bool {
	switch {
	case c.Adult == AdultGeneralOnly && it.Adult, c.Adult == AdultOnly && !it.Adult:
		return false
	case c.Uncensored == CensoredOnly && it.Uncensored, c.Uncensored == UncensoredOnly && !it.Uncensored:
		return false
	}
	return true
}

// matchesWhere: size, memory, license, installation, where it runs and who serves it.
func (c Condition) matchesWhere(it Item) bool {
	switch {
	case len(c.Params) > 0 && !inBands(it.ParamsB, c.Params):
		return false
	case c.VRAMFit && (it.VRAMMinGB <= 0 || it.VRAMMinGB > VRAMFitGB):
		return false
	case !c.allowsCommercial(it.Commercial):
		return false
	case c.Installed == InstalledOnly && !it.Installed, c.Installed == NotInstalledOnly && it.Installed:
		return false
	}
	return (len(c.Origins) == 0 || slices.Contains(c.Origins, it.Origin)) && (len(c.Providers) == 0 || slices.Contains(c.Providers, it.Provider))
}

// matchesGuides: the speed and quality guides; a model with no guide matches none.
func (c Condition) matchesGuides(it Item) bool {
	return (len(c.Speeds) == 0 || slices.Contains(c.Speeds, it.Speed)) && (len(c.Qualities) == 0 || slices.Contains(c.Qualities, it.Quality))
}

// Apply keeps the items that match, in their order. A nil result is an empty list.
func (c Condition) Apply(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if c.Match(it) {
			out = append(out, it)
		}
	}
	return out
}

// Filter keeps the elements of list whose item (item(element)) matches, in their order: the helper the screens and the tools use to narrow their own
// lists without a second pass.
func Filter[T any](c Condition, list []T, item func(T) Item) []T {
	out := make([]T, 0, len(list))
	for _, e := range list {
		if c.Match(item(e)) {
			out = append(out, e)
		}
	}
	return out
}

func hasAll(have, want []traits.Input) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func inBands(billions float64, bands []Band) bool {
	b, known := BandOf(billions)
	return known && slices.Contains(bands, b)
}

func (c Condition) allowsCommercial(a modellicense.Commercial) bool {
	set := c.CommercialSet()
	return len(set) == 0 || slices.Contains(set, a)
}

// matchesText: every word of the search (split at spaces, lower case) is in the name, the ID, the provider or a tag.
func (c Condition) matchesText(it Item) bool {
	words := strings.Fields(strings.ToLower(c.Text))
	if len(words) == 0 {
		return true
	}
	hay := strings.ToLower(strings.Join(append([]string{it.Name, it.ID, it.Provider}, it.Tags...), " "))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// Active is how many parts of the condition are set: the number on the filter icon's badge. The commercial chips count as one, the text search counts
// as one, and "all" of a three-way part counts as none. AvailableOnly is not counted: it has its own switch ("選べるものだけ"), which the screen shows
// as a chip when it is not at its starting point.
func (c Condition) Active() int {
	n := 0
	for _, set := range []bool{
		len(c.Kinds) > 0, len(c.Inputs) > 0, c.Adult != "" && c.Adult != AdultAll, c.Uncensored != "" && c.Uncensored != UncensoredAll,
		len(c.Params) > 0, c.VRAMFit, len(c.CommercialSet()) > 0, c.Installed != "" && c.Installed != InstalledAll, len(c.Origins) > 0, len(c.Providers) > 0,
		c.Audio, len(c.Speeds) > 0, len(c.Qualities) > 0, strings.TrimSpace(c.Text) != "",
	} {
		if set {
			n++
		}
	}
	return n
}

// CatalogInfo is what the model manager's catalog says of a local model beyond its inputs, for the lists that are not the catalog's own (the generation
// test's models are found by their workflow's name, modelmanager.Service.InfoOf): the content rating, the traits and the VRAM guideline.
type CatalogInfo struct {
	Adult     bool
	Traits    traits.Traits
	VRAMMinGB int
}
