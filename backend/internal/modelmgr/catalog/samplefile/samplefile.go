// Package samplefile gives a workflow template the sampling values a model's author recommends (catalog.Workflow.Sampling). Like mainfile, it
// rewrites the literals of the template's graph, in the model that is saved for the catalog entry, and leaves the template as it is.
package samplefile

import (
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Model is the model with the sampling values of s written in place of the ones its graph holds (the literal at the first assignment of
// the value that is not a Ratio one: where SamplingCaps reads the default from). It is how a catalog model that shares a template with others
// runs with what its author recommends (Issue #994): the request still overrides them, and a request that names nothing runs these. The
// template is not changed. A value the workflow does not take (no such assignment, or the steps and the cfg of a speed mode) is an error: the
// catalog says what the model runs with, so it must be able to.
func Model(m localcfg.Model, s localcfg.SamplingSettings) (localcfg.Model, error) {
	if s.IsZero() {
		return m, nil
	}
	if err := s.Validate(); err != nil {
		return localcfg.Model{}, err
	}
	if m.Workflow == nil {
		return localcfg.Model{}, fmt.Errorf("a model without a workflow takes no sampling values")
	}
	g, err := localcfg.ParseGraph(m.Workflow.Graph)
	if err != nil {
		return localcfg.Model{}, err
	}
	locked := len(m.Workflow.SpeedModes) > 0
	put := func(p localcfg.Param, v any) error {
		if v == nil {
			return nil
		}
		if locked && (p == localcfg.ParamSteps || p == localcfg.ParamCFG) {
			return fmt.Errorf("%s is the speed mode's, the workflow cannot take it", p)
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		for _, a := range m.Workflow.Assignments {
			if a.Param != p || a.Ratio != 0 || g[a.Node].Inputs[a.Input] == nil {
				continue
			}
			n := g[a.Node]
			inputs := make(map[string]json.RawMessage, len(n.Inputs))
			for k, in := range n.Inputs {
				inputs[k] = in
			}
			inputs[a.Input] = raw
			n.Inputs = inputs
			g[a.Node] = n
			return nil
		}
		return fmt.Errorf("the workflow has no %s to put the value in", p)
	}
	for _, v := range []struct {
		p localcfg.Param
		v any
	}{{localcfg.ParamSteps, ptrAny(s.Steps)}, {localcfg.ParamCFG, ptrAny(s.CFG)}, {localcfg.ParamSampler, textAny(s.Sampler)}, {localcfg.ParamScheduler, textAny(s.Scheduler)}} {
		if err := put(v.p, v.v); err != nil {
			return localcfg.Model{}, err
		}
	}
	graph, err := json.Marshal(g)
	if err != nil {
		return localcfg.Model{}, err
	}
	wf := *m.Workflow
	wf.Graph = graph
	m.Workflow = &wf
	return m, nil
}

// ptrAny is the value p points to, or nil (any, so that a nil pointer is "not given" and not a typed nil).
func ptrAny[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func textAny(s string) any {
	if s == "" {
		return nil
	}
	return s
}
