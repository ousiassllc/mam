// Package exposedgate is the age gate for the values of the "exposed" inputs of a generation test request (Issue #977): HidesModel judges a model by
// the files of its graph as it stands, but a value of a choice input (a LoRA, a checkpoint, a diffusion model) puts another file in at run time.
package exposedgate

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// Hides returns the predicate "the model, run with these exposed values (by key "<node>.<input>"), is hidden": while the gate is closed, a value that
// names a file only an adult model has hides the model, though its own graph reads no such file (gentest.Deps.HiddenWith). A value the workflow does
// not accept hides nothing here: the run is refused for it anyway.
func Hides(mgr *modelmanager.Service) func(id string, values map[string]any) bool {
	return func(id string, values map[string]any) bool {
		if len(values) == 0 || mgr.Adapter == nil || mgr.AdultContentEnabled() {
			return false
		}
		m, _, ok := mgr.Adapter.Config().LocalModel(domain.ModelName(id))
		if !ok || m.Workflow == nil {
			return false
		}
		g, err := m.Workflow.GraphWith(values)
		if err != nil {
			return false
		}
		raw, err := json.Marshal(g)
		return err == nil && mgr.AdultFiles().Reads(raw)
	}
}
