// Package sampled is how the sampling values of a request (steps, cfg, sampler, scheduler) reach the nodes of a ComfyUI graph: which of them the workflow
// takes, and the value an assignment puts into its node. It is a package of its own because package comfyui is at its line limit.
package sampled

import (
	"math"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Applicable is the part of the request's sampling the workflow takes (SamplingCaps.Applicable): a value without an assignment, and the
// steps and the cfg of a workflow with speed modes (the mode owns them), are not sent. What is left is put into the nodes the assignments name; every
// value not named stays as the graph has it, so a request that names none runs the workflow exactly as it stands.
func Applicable(w localcfg.Workflow, req localcfg.SamplingSettings) localcfg.SamplingSettings {
	if req.IsZero() {
		return req
	}
	return w.SamplingCaps().Applicable(req)
}

// Check refuses the request's sampling when the workflow cannot run it: a two-stage workflow (Wan 2.2) needs at least two steps, one for each stage.
func Check(w localcfg.Workflow, req localcfg.SamplingSettings) error {
	if req.IsZero() {
		return nil
	}
	return req.ValidateFor(w.SamplingCaps())
}

// Value is the value an assignment of a sampling parameter puts into its node; ok is false when the request names none (the graph's value
// stays). isSampling is false for the other parameters.
func Value(a localcfg.Assignment, s localcfg.SamplingSettings) (value any, ok, isSampling bool) {
	switch a.Param {
	case localcfg.ParamSteps:
		if s.Steps == nil {
			return nil, false, true
		}
		if a.Ratio > 0 {
			return handover(*s.Steps, a.Ratio), true, true
		}
		return *s.Steps, true, true
	case localcfg.ParamCFG:
		if s.CFG == nil {
			return nil, false, true
		}
		return *s.CFG, true, true
	case localcfg.ParamSampler:
		return s.Sampler, s.Sampler != "", true
	case localcfg.ParamScheduler:
		return s.Scheduler, s.Scheduler != "", true
	}
	return nil, false, false
}

// handover is the step at which a two-stage sampler hands over to its second part: round(steps*ratio), at least 1 and, when there is a second part
// to run at all (two steps or more), before the last step.
func handover(steps int, ratio float64) int {
	n := int(math.Round(float64(steps) * ratio))
	return min(max(n, 1), max(steps-1, 1))
}
