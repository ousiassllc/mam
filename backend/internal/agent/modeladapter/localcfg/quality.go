package localcfg

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
)

// validatePresets checks the quality presets of a workflow (Issue #977): each names a value the workflow takes (an assignment of the
// steps or the cfg), within the ranges of a request, and a workflow whose speed modes own the steps and the cfg has none. The fast steps
// lie below the workflow's own and the high steps above them, so that the buttons do what they say.
func validatePresets(where string, w Workflow) []error {
	p := w.QualityPresets
	if p == nil {
		return nil
	}
	var errs []error
	at := where + ": quality_presets"
	if p.None() {
		errs = append(errs, fmt.Errorf("%s: names neither fast nor high", at))
	}
	if len(w.SpeedModes) > 0 {
		errs = append(errs, fmt.Errorf("%s: a workflow with speed_modes has none (the mode owns the steps and the cfg)", at))
	}
	caps := w.SamplingCaps()
	check := func(name string, preset *sampling.Preset, below bool) {
		if preset == nil {
			return
		}
		if preset.Steps == nil && preset.CFG == nil {
			errs = append(errs, fmt.Errorf("%s.%s: names neither steps nor cfg", at, name))
			return
		}
		if preset.Steps != nil && (caps == nil || caps.Steps == nil) {
			errs = append(errs, fmt.Errorf("%s.%s: the workflow has no steps assignment", at, name))
		}
		if preset.CFG != nil && (caps == nil || caps.CFG == nil) {
			errs = append(errs, fmt.Errorf("%s.%s: the workflow has no cfg assignment", at, name))
		}
		if err := preset.Settings().ValidateFor(caps); err != nil {
			errs = append(errs, fmt.Errorf("%s.%s: %w", at, name, err))
		}
		if s := preset.Steps; s != nil && caps != nil && caps.Steps != nil {
			if def := caps.Steps.Default; (below && float64(*s) >= def) || (!below && float64(*s) <= def) {
				errs = append(errs, fmt.Errorf("%s.%s: steps %d must be %s than the workflow's %g", at, name, *s, map[bool]string{true: "lower", false: "higher"}[below], def))
			}
		}
	}
	check("fast", p.Fast, true)
	check("high", p.High, false)
	return errs
}
