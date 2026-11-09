package loras

import "github.com/ousiassllc/mam/backend/internal/domain/modellicense"

// Meta is what the model manager knows of a LoRA the user added from their own PC (#1119): the families of base model it is made for
// (lorafamily keys), the recommended strength, the trigger words and its license. Nothing the user did not confirm is filled in: an
// empty field is "not set", and License is Unknown unless the user said otherwise.
type Meta struct {
	BaseModels   []string
	Strength     *float64
	TriggerWords []string
	License      modellicense.Info
}

// Lookup finds the Meta of the LoRA the engine lists under a file name (false: the file was not added through the model manager).
type Lookup func(name string) (Meta, bool)

// Choice is a LoRA of the engine with what is known of it (nil Meta: nothing: a file put in the folder by hand, or a catalog model's).
type Choice struct {
	Name string
	Meta *Meta
}

// Choices pairs the names the engine lists with what lookup knows of each (a nil lookup knows nothing).
func Choices(names []string, lookup Lookup) []Choice {
	out := make([]Choice, 0, len(names))
	for _, n := range names {
		c := Choice{Name: n}
		if lookup != nil {
			if m, ok := lookup(n); ok {
				c.Meta = &m
			}
		}
		out = append(out, c)
	}
	return out
}
