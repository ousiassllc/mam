// Package runtimechoices is the choice of the model of a local runtime that has several (Irodori-TTS: v4.1-Small, v4-Large, ...; spec/architecture/
// model-manager/local-runtime.md §14): what the models are, which one is in force, and the choice of another. The weights of the model in
// force are the ones the runtime checks and fetches (localruntime.Service.InstallWeights) and the server starts with; choosing another stops
// the server, so that its next start loads the other model. It is a package of its own because package localruntime is at its line budget.
//
// A model whose license has to be accepted (the Gemma Terms of the Large models) is chosen only after the user accepted it: the acceptance is
// a value the caller passes (Select's accept) and is recorded with the version of the text that was shown, like the consent of the model
// manager (store.Consent). Nothing here accepts for the user, and no code of mam passes accept on its own.
package runtimechoices

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/vramfit"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// Service chooses among the models of the runtimes of a localruntime.Service.
type Service struct {
	Runtimes *localruntime.Service
}

// View is one model of a runtime with where it stands: what it is, its license and restrictions, the video memory it needs against this
// GPU, whether it is the one in force, and whether its license still has to be accepted.
type View struct {
	choice.Choice
	// Active: the runtime runs with this model.
	Active bool `json:"active"`
	// SizeBytes is the weights to fetch for the model (its own files and what every model shares).
	SizeBytes int64 `json:"size_bytes"`
	// License is the license of the model, as the weights list shows it.
	LicenseInfo catalog.ResourceLicense `json:"license_info"`
	// ConsentNeeded: the model has a license to accept that the record does not hold in its current version. Accepted is the opposite
	// for a model that has one.
	ConsentNeeded bool `json:"consent_needed"`
	Accepted      bool `json:"accepted"`
	// VRAMFit says how this PC's GPU stands to the model's guideline ("" when the memory is not known or the model gives no recommendation).
	VRAMFit string `json:"vram_fit,omitempty"`
}

// Accept is the acceptance of the license of a model: the version of the text the user was shown (choice.Consent.Version). It is a value
// the user's own action puts in (the checkbox of the screen, the argument the user told the assistant to pass); it is never made up.
type Accept struct{ Version string }

// Views lists the models of the runtime with the one in force marked (an empty list for a runtime without choices); ErrNotFound for
// another id.
func (s Service) Views(ctx context.Context, runtimeID string) ([]View, error) {
	set, st, err := s.load(runtimeID)
	if err != nil {
		return nil, err
	}
	gpuBytes := hfplan.VRAMOf(s.Runtimes.GPUReport(ctx).Adapters)
	chosen := set.Chosen(st.Choice)
	out := make([]View, 0, len(set.Choices))
	for _, c := range set.Choices {
		lic, _ := set.License(c.License)
		v := View{Choice: c, Active: c.ID == chosen, SizeBytes: sizeOf(set, c.ID), LicenseInfo: lic}
		if c.Consent != nil {
			v.Accepted = st.Consents[c.License].Version == c.Consent.Version
			v.ConsentNeeded = !v.Accepted
		}
		if c.VRAMRecommendedGB > c.VRAMMinGB && c.VRAMLimits != "" { // only a model that says what a smaller GPU gives up is judged
			v.VRAMFit = vramfit.Text(catalog.VRAM{MinGB: c.VRAMMinGB, RecommendedGB: c.VRAMRecommendedGB, Limits: c.VRAMLimits}, gpuBytes)
		}
		out = append(out, v)
	}
	return out, nil
}

// Select makes the model the one the runtime runs with and stops the server (a stopped one is a no-op), so that the next start loads it. The
// weights of the model are not fetched here: the runtime says they are missing until "重みを導入" (InstallWeights) fetched them. A model that
// is not one of the runtime's is choice.ErrUnknown; one whose license is not accepted is choice.ErrConsentRequired unless accept holds the
// version of its text (then the acceptance is recorded); an install in progress is localruntime.ErrBusy (its files are being replaced).
// A wrong version is an error and records nothing.
func (s Service) Select(ctx context.Context, runtimeID, choiceID string, accept *Accept) error {
	set, st, err := s.load(runtimeID)
	if err != nil {
		return err
	}
	var c *choice.Choice
	for i := range set.Choices {
		if set.Choices[i].ID == choiceID {
			c = &set.Choices[i]
		}
	}
	if c == nil {
		return fmt.Errorf("%w: %q", choice.ErrUnknown, choiceID)
	}
	if v, err := s.Runtimes.Status(ctx, runtimeID); err != nil {
		return err
	} else if v.InstallJob != "" {
		return localruntime.ErrBusy
	}
	now := time.Now()
	if s.Runtimes.Now != nil {
		now = s.Runtimes.Now()
	}
	consent := c.Consent != nil && st.Consents[c.License].Version != c.Consent.Version
	if consent && (accept == nil || accept.Version != c.Consent.Version) {
		return choice.ErrConsentRequired
	}
	if err := s.Runtimes.Update(runtimeID, func(r *store.RuntimeState) {
		r.Choice = choiceID
		if consent {
			if r.Consents == nil {
				r.Consents = map[string]store.Consent{}
			}
			r.Consents[c.License] = store.Consent{Version: c.Consent.Version, AcceptedAt: now.UTC()}
		}
	}); err != nil {
		return err
	}
	return s.Runtimes.Stop(runtimeID)
}

// load is the weights set of the runtime and its record.
func (s Service) load(runtimeID string) (catalog.ResourceSet, store.RuntimeState, error) {
	st, err := s.Runtimes.State(runtimeID)
	if err != nil {
		return catalog.ResourceSet{}, st, err
	}
	set, _ := s.Runtimes.Catalog.ResourceSet(runtimeID)
	return set, st, nil
}

// sizeOf is the bytes to fetch for the model: its own assets and the ones every model shares.
func sizeOf(set catalog.ResourceSet, id string) int64 {
	set.Assets = set.Active(id)
	return set.TotalSize()
}
