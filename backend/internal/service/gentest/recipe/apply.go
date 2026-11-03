package recipe

import (
	"context"
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// Apply fills the request from the recipe: the model, and every setting the request does not name itself (a value the request names wins: the steps,
// the cfg, the sampler, the scheduler, the seed, the quality, the size, the speed mode, the LoRA, the exposed values, the negative prompt of a prompt that has none and
// the strength of an input image that has none). A request that names models as well is refused, as one with an intent is; so is a recipe whose model
// cannot be used now (the reason is the message). r is returned changed, not modified.
func (s *Service) Apply(ctx context.Context, r gentest.Request, id string) (gentest.Request, View, error) {
	v, err := s.Get(ctx, id)
	if err != nil {
		return r, v, err
	}
	if len(r.Models) > 0 {
		return r, v, gterr.Invalid("models と recipe は同時に指定できません（recipe はモデルをレシピが決める指定です）")
	}
	if r.Media != "" && r.Media != gentest.MediaImage {
		return r, v, gterr.Invalid("recipe は画像だけで指定できます")
	}
	if !v.Usable {
		return r, v, gterr.Invalid("レシピ「%s」は使えません: %s", v.Name, v.Reason)
	}
	m, _ := s.status(ctx, v.Params.Model)
	return Fill(r, withCurrentExposed(v.Params, m)), v, nil
}

// withCurrentExposed drops the exposed values the model's workflow no longer has, or no longer accepts, and the speed mode it no longer offers (it was
// edited since): a recipe stays usable.
func withCurrentExposed(p Params, m modelcatalog.Info) Params {
	if !m.HasSpeedMode(p.SpeedMode) {
		p.SpeedMode = ""
	}
	kept := map[string]any{}
	for _, c := range m.Exposed {
		if v, ok := p.Exposed[c.Key]; ok {
			if _, err := c.Coerce(v); err == nil {
				kept[c.Key] = v
			}
		}
	}
	p.Exposed = kept
	return p
}

// Fill is Apply without the checks: the request with the settings of the params that it does not name.
func Fill(r gentest.Request, p Params) gentest.Request {
	r.Models = []string{p.Model}
	s := &r.Sampling
	if s.Steps == nil {
		s.Steps = p.Sampling.Steps
	}
	if s.CFG == nil {
		s.CFG = p.Sampling.CFG
	}
	if s.Sampler == "" {
		s.Sampler = p.Sampling.Sampler
	}
	if s.Scheduler == "" {
		s.Scheduler = p.Sampling.Scheduler
	}
	if r.Quality == "" {
		r.Quality = p.Quality
	}
	if r.Seed == nil {
		r.Seed = p.Seed
	}
	if r.AspectRatio == "" {
		r.AspectRatio = p.AspectRatio
	}
	if r.LoRA == nil {
		r.LoRA = p.LoRA
	}
	if r.SpeedMode == "" {
		r.SpeedMode = p.SpeedMode
	}
	r.Prompts = slices.Clone(r.Prompts)
	for i := range r.Prompts {
		if r.Prompts[i].Negative == "" {
			r.Prompts[i].Negative = p.Negative
		}
	}
	r.References = slices.Clone(r.References)
	for i := range r.References {
		if r.References[i].Strength == nil {
			r.References[i].Strength = p.ReferenceStrength
		}
	}
	exposed := maps.Clone(p.Exposed)
	if exposed == nil {
		exposed = map[string]any{}
	}
	maps.Copy(exposed, r.Exposed) // what the request names wins
	if len(exposed) > 0 {
		r.Exposed = exposed
	}
	return r
}
