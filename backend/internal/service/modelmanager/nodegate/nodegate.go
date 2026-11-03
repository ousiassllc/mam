// Package nodegate is the check that a model whose workflows use custom nodes (localcfg.Workflow.RequiresNodes) is not installed, or chosen,
// while those nodes are not installed (spec/architecture/model-manager/custom-nodes.md §6). It is a package of its own because package
// modelmanager is at its size limit; modelmanager.Service.InstallModel, the generation test's model list and the screens all ask it.
package nodegate

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// Section is where the nodes are installed: the section "追加ノード" of the model manager screen.
const Section = "/model-manager#mm-nodes"

// templateNodes is the nodes each template requires, by template ID. Reading the templates parses them all, so it is done once.
var templateNodes = sync.OnceValue(func() map[string][]string {
	out := map[string][]string{}
	for _, t := range localcfg.Templates() {
		if t.Model.Workflow != nil && len(t.Model.Workflow.RequiresNodes) > 0 {
			out[t.ID] = t.Model.Workflow.RequiresNodes
		}
	}
	return out
})

// Required is the IDs of the custom nodes the workflows of the catalog model need, sorted.
func Required(m catalog.Model) []string {
	var out []string
	for _, w := range m.Workflows {
		out = append(out, templateNodes()[w.Template]...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Missing is the nodes of ids that the engine does not load now, with their names (an ID of the allowlist's gives its name).
func Missing(mgr *nodes.Manager, ids []string) []string {
	var names []string
	for _, id := range mgr.Missing(ids) {
		name := id
		if n, ok := mgr.Allowlist().Node(id); ok {
			name = n.Name
		}
		names = append(names, name)
	}
	return names
}

// Message is the sentence for a workflow, or a model, that needs nodes that are not installed ("" when there is none missing).
func Message(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("追加ノード「%s」が導入されていないため使えません。モデルマネージャーの「追加ノード」の節（%s）で導入してください", strings.Join(missing, "」「"), Section)
}

// Check is nil when every node the model's workflows need is installed; else an *InvalidError (the refusal of the install, before a job is made).
func Check(mgr *nodes.Manager, m catalog.Model) error {
	if msg := Message(Missing(mgr, Required(m))); msg != "" {
		return &modelsettings.InvalidError{Message: m.Name + " は、" + msg}
	}
	return nil
}

// ForModels is the predicate the generation test lists models with: the reason a model of the engine's endpoint cannot be chosen because a node
// its workflow requires is not installed ("" when it can). A model of any other endpoint (an external ComfyUI, which has its own nodes) and a model
// that requires none always can.
func ForModels(mgr *nodes.Manager, endpoint string) func(modelID string, required []string) string {
	return func(modelID string, required []string) string {
		if _, ep, _, ok := localcfg.ParseID(modelID); !ok || ep != endpoint || len(required) == 0 {
			return ""
		}
		return Message(Missing(mgr, required))
	}
}
