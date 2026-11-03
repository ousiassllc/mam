package localcfg

import (
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
)

// The sampling values a request can override (sampling.Settings); a run that names none is the graph as it stands.
const (
	ParamSteps     Param = "steps"
	ParamCFG       Param = "cfg"
	ParamSampler   Param = "sampler"
	ParamScheduler Param = "scheduler"
)

// SamplingSettings are the sampling values a request overrides (see package sampling).
type SamplingSettings = sampling.Settings

// SamplingCaps lists the sampling values the workflow takes with their defaults (the literal the graph holds where the first assignment
// of the value that is not a Ratio one puts it); nil when it takes none. A workflow with speed modes locks the steps and the cfg.
func (w Workflow) SamplingCaps() *sampling.Caps {
	g, err := ParseGraph(w.Graph)
	if err != nil {
		return nil
	}
	lock := ""
	if len(w.SpeedModes) > 0 {
		lock = sampling.LockedBySpeedMode
	}
	literal := func(p Param) json.RawMessage {
		for _, a := range w.Assignments {
			if v := g[a.Node].Inputs[a.Input]; a.Param == p && a.Ratio == 0 && v != nil {
				return v
			}
		}
		return nil
	}
	number := func(p Param) *sampling.NumberCap {
		var f float64
		if v := literal(p); v != nil && json.Unmarshal(v, &f) == nil {
			return &sampling.NumberCap{Default: f, Locked: lock}
		}
		return nil
	}
	name := func(p Param) *sampling.NameCap {
		var s string
		if v := literal(p); v != nil && json.Unmarshal(v, &s) == nil {
			return &sampling.NameCap{Default: s}
		}
		return nil
	}
	c := &sampling.Caps{Steps: number(ParamSteps), CFG: number(ParamCFG), Sampler: name(ParamSampler), Scheduler: name(ParamScheduler)}
	if c.None() {
		return nil
	}
	for _, a := range w.Assignments {
		c.TwoStage = c.TwoStage || (a.Param == ParamSteps && a.Ratio > 0 && c.Steps != nil && lock == "")
	}
	if lock == "" && !w.QualityPresets.None() {
		c.Presets = w.QualityPresets // the steps and the cfg of a workflow under a speed mode are the mode's: no quality to choose
	}
	return c
}

// validateSampling checks the assignments of the sampling values: the input holds a number (steps, cfg) or a text (sampler,
// scheduler), and a Ratio belongs to a steps assignment and lies between 0 and 1.
func validateSampling(where string, w Workflow, g Graph) []error {
	var errs []error
	for i, a := range w.Assignments {
		at := fmt.Sprintf("%s: assignments[%d]", where, i)
		if a.Ratio != 0 && (a.Param != ParamSteps || !(a.Ratio > 0 && a.Ratio < 1)) {
			errs = append(errs, fmt.Errorf("%s: ratio applies to steps only and lies between 0 and 1", at))
		}
		lit := g[a.Node].Inputs[a.Input]
		if lit == nil || IsLink(lit) {
			continue // reported by the assignment check
		}
		var f float64
		var s string
		switch {
		case (a.Param == ParamSteps || a.Param == ParamCFG) && json.Unmarshal(lit, &f) != nil:
			errs = append(errs, fmt.Errorf("%s: %s goes into a number input, input %q of node %s holds %s", at, a.Param, a.Input, a.Node, lit))
		case (a.Param == ParamSampler || a.Param == ParamScheduler) && json.Unmarshal(lit, &s) != nil:
			errs = append(errs, fmt.Errorf("%s: %s goes into a text input, input %q of node %s holds %s", at, a.Param, a.Input, a.Node, lit))
		}
	}
	return append(errs, validatePresets(where, w)...)
}
