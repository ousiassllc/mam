package localcfg

import (
	"encoding/json"
	"fmt"
)

// WithSampling is the model with the sampling values of s written in place of the ones its graph holds (the literal at the first assignment of
// the value that is not a Ratio one: where SamplingCaps reads the default from). It is how a catalog model that shares a template with others
// runs with what its author recommends (Issue #994): the request still overrides them, and a request that names nothing runs these. The
// template is not changed. A value the workflow does not take (no such assignment, or the steps and the cfg of a speed mode) is an error: the
// catalog says what the model runs with, so it must be able to.
func (m Model) WithSampling(s SamplingSettings) (Model, error) {
	if s.IsZero() {
		return m, nil
	}
	if err := s.Validate(); err != nil {
		return Model{}, err
	}
	if m.Workflow == nil {
		return Model{}, fmt.Errorf("a model without a workflow takes no sampling values")
	}
	g, err := ParseGraph(m.Workflow.Graph)
	if err != nil {
		return Model{}, err
	}
	locked := len(m.Workflow.SpeedModes) > 0
	put := func(p Param, v any) error {
		if v == nil {
			return nil
		}
		if locked && (p == ParamSteps || p == ParamCFG) {
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
		p Param
		v any
	}{{ParamSteps, ptrAny(s.Steps)}, {ParamCFG, ptrAny(s.CFG)}, {ParamSampler, textAny(s.Sampler)}, {ParamScheduler, textAny(s.Scheduler)}} {
		if err := put(v.p, v.v); err != nil {
			return Model{}, err
		}
	}
	graph, err := json.Marshal(g)
	if err != nil {
		return Model{}, err
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
