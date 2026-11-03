package redo

import (
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
)

// OfRecipe reads the settings of a recipe in the terms of the form (Issue #977: "使う" puts a recipe into the detail tier). A text field is "" when the
// recipe does not hold the value; RefStrength is the strength the input images that have none take.
func OfRecipe(p recipe.Params) Restored {
	r := Restored{Model: p.Model, Negative: p.Negative, Aspect: p.AspectRatio, LoRA: p.LoRA, SpeedMode: p.SpeedMode, Sampler: p.Sampling.Sampler, Scheduler: p.Sampling.Scheduler}
	if p.Seed != nil {
		r.Seed = strconv.FormatInt(*p.Seed, 10)
	}
	if s := p.Sampling.Steps; s != nil {
		r.Steps = strconv.Itoa(*s)
	}
	if c := p.Sampling.CFG; c != nil {
		r.CFG = strconv.FormatFloat(*c, 'f', -1, 64)
	}
	for k, v := range p.Exposed {
		if r.Exposed == nil {
			r.Exposed = map[string]string{}
		}
		r.Exposed[k] = textOf(v)
	}
	r.RefStrength = p.ReferenceStrength
	return r
}

func textOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}
