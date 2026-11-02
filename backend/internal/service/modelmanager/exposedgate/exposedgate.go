// Package exposedgate is the age gate for the values of the "exposed" inputs of a generation test request (Issue #977): HidesModel judges a model by
// the files of its graph as it stands, but a value of a choice input (a LoRA, a checkpoint, a diffusion model) puts another file in at run time.
package exposedgate

import (
	"encoding/json"
	"slices"

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

// Choices returns the filter of the choices of a choice input (gentest.Deps.HideChoices): while the gate is closed, it leaves out the choices that
// name a file only an adult model has, but the one it is told to keep (the value the graph holds). A choice is judged by its own value, on a graph of one node, so it works for an input that is not exposed yet
// (the picker offers it) as for one that is (the choices the screen and list_generation_models show).
func Choices(mgr *modelmanager.Service) func(options []string, keep string) []string {
	return func(options []string, keep string) []string {
		if len(options) == 0 || mgr.AdultContentEnabled() {
			return options
		}
		files := mgr.AdultFiles()
		return slices.DeleteFunc(slices.Clone(options), func(o string) bool {
			raw, err := json.Marshal(map[string]any{"n": map[string]any{"class_type": "x", "inputs": map[string]any{"i": o}}})
			return o != keep && err == nil && files.Reads(raw)
		})
	}
}
