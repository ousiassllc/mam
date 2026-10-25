// Package adult judges what an adult model is: the names of the models the model manager created for it, and the files it
// reads (spec/architecture/model-manager/model-manager.md §6.1). The age gate asks it for every model of the bundled endpoint
// and for every other model whose workflow reads one of those files, so that a model that was renamed, an imported graph, a
// saved workflow or a model on another ComfyUI endpoint that reads the same file is judged as the adult model it is.
// It depends on the catalog and the record of the model manager only.
package adult

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// Workflows are the names of the models of the bundled endpoint that belong to an adult model: the workflows of the adult
// catalog models, of the installed models recorded as adult (their record, so that a later catalog that drops or renames
// the model does not give the model away) and of the NFAA models added from Hugging Face.
func Workflows(cat *catalog.Catalog, st store.State) map[string]bool {
	out := map[string]bool{}
	for _, m := range cat.Models {
		if m.Adult() {
			for _, w := range m.Workflows {
				out[w.Name] = true
			}
		}
	}
	for id, rec := range st.Models {
		if cm, inCatalog := cat.Model(id); rec.Adult || (inCatalog && cm.Adult()) { // a record from before Adult was kept has the catalog's word
			for _, name := range rec.Workflows {
				out[name] = true
			}
		}
	}
	for _, um := range st.UserModels {
		if um.NFAA {
			for _, name := range um.Workflows {
				out[name] = true
			}
		}
	}
	return out
}

// Files are the files only an adult model has: those of the adult catalog models, of the installed models recorded as adult
// and of the NFAA models added from Hugging Face, less every file a general model has too (the Flux.1 Dev checkpoint, the
// Wan 2.2 base and text encoder are shared with the adult models that build on them: a workflow that reads only those is
// not an adult one). A file is known by its name below its folder, in lower case: ComfyUI lists a file of a sub folder as
// "<sub>/<name>" and a Windows file system does not tell the case.
type Files map[string]bool

// FilesOf collects the Files of the catalog and the record.
func FilesOf(cat *catalog.Catalog, st store.State) Files {
	adults, general := map[string]bool{}, map[string]bool{}
	mark := func(isAdult bool, name string) {
		if isAdult {
			adults[key(name)] = true
		} else {
			general[key(name)] = true
		}
	}
	for _, m := range cat.Models {
		for _, f := range m.Files {
			mark(m.Adult(), f.Name)
		}
	}
	for id, rec := range st.Models {
		cm, inCatalog := cat.Model(id)
		for _, f := range rec.Files {
			mark(rec.Adult || (inCatalog && cm.Adult()), path.Base(f.Path))
		}
	}
	for _, um := range st.UserModels {
		mark(um.NFAA, um.Name)
	}
	out := Files{}
	for name := range adults {
		if !general[name] {
			out[name] = true
		}
	}
	return out
}

// Reads reports whether the graph (API format) reads one of the files: a loader input (a checkpoint, a diffusion model, a
// LoRA, a VAE or a text encoder; any string input of any node, so that a custom loader node is judged as well) that names it.
// A graph that cannot be read reads nothing: it cannot run either.
func (f Files) Reads(graph json.RawMessage) bool {
	if len(f) == 0 || len(graph) == 0 {
		return false
	}
	g, err := localcfg.ParseGraph(graph)
	if err != nil {
		return false
	}
	for _, node := range g {
		for _, raw := range node.Inputs {
			var s string
			if json.Unmarshal(raw, &s) == nil && f[key(s)] {
				return true
			}
		}
	}
	return false
}

// key is the name of a file below its folder, in lower case ("sub\\Name.safetensors" is "name.safetensors").
func key(name string) string {
	return strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
}
