package localruntime

import (
	"context"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// State is the record of the runtime: the model the user chose and the licenses accepted (package runtimechoices reads and writes them).
func (s *Service) State(id string) (store.RuntimeState, error) {
	inst, err := s.instance(id)
	if err != nil {
		return store.RuntimeState{}, err
	}
	return inst.state.Get(), nil
}

// Update changes the record of the runtime.
func (s *Service) Update(id string, edit func(*store.RuntimeState)) error {
	inst, err := s.instance(id)
	if err != nil {
		return err
	}
	return inst.state.Update(edit)
}

// GPUReport is the adapters of this machine from the detection Status uses (remembered for a minute).
func (s *Service) GPUReport(ctx context.Context) gpu.Report { return s.report(ctx, false) }

// chosen is the model the runtime runs with (nil for a runtime without choices).
func (inst *instance) chosen() *choice.Choice {
	set := inst.install.AssetSet
	i := slices.IndexFunc(set.Choices, func(c choice.Choice) bool { return c.ID == set.Chosen(inst.state.Get().Choice) })
	if i < 0 {
		return nil
	}
	return &set.Choices[i]
}

// needConsent is choice.ErrConsentRequired when the model in force has a license the record has not accepted (in its current version).
func (inst *instance) needConsent() error {
	c := inst.chosen()
	if c == nil || c.Consent == nil || inst.state.Get().Consents[c.License].Version == c.Consent.Version {
		return nil
	}
	return choice.ErrConsentRequired
}
