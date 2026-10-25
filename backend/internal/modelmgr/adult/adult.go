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
	"unicode"
	"unicode/utf8"

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
// and of the NFAA models added from Hugging Face, less every file a general model of the catalog has too (the Flux.1 Dev
// checkpoint, the Wan 2.2 base and text encoder are shared with the adult models that build on them: a workflow that reads
// only those is not an adult one). Only the catalog subtracts: a same-named file that a user record or an untagged import
// has does not make the name of an adult file a general one. A file is known by its name below its folder, in lower case:
// ComfyUI lists a file of a sub folder as "<sub>/<name>" and a Windows file system does not tell the case.
type Files map[string]bool

// FilesOf collects the Files of the catalog and the record.
func FilesOf(cat *catalog.Catalog, st store.State) Files {
	adults, general := map[string]bool{}, map[string]bool{}
	for _, m := range cat.Models {
		for _, f := range m.Files {
			if m.Adult() {
				adults[key(f.Name)] = true
			} else {
				general[key(f.Name)] = true
			}
		}
	}
	for id, rec := range st.Models {
		cm, inCatalog := cat.Model(id)
		if !rec.Adult && (!inCatalog || !cm.Adult()) {
			continue
		}
		for _, f := range rec.Files {
			adults[key(path.Base(f.Path))] = true
		}
	}
	for _, um := range st.UserModels {
		if um.NFAA {
			adults[key(um.Name)] = true
		}
	}
	out := Files{}
	for name := range adults {
		if !general[name] {
			out[name] = true
		}
	}
	return out
}

// Reads reports whether the graph (API format) reads one of the files: an input of any node (a checkpoint, a diffusion
// model, a LoRA, a VAE or a text encoder of a loader; any value of any node, so that a custom loader node is judged as
// well) that names it. Every value is walked down to its string leaves, so a file that sits in an object (a rgthree Power
// Lora Loader) or a list is read too, and a leaf that is not a bare file name (a "<lora:Name:0.8>" tag of a prompt, a name
// with a space around it) is read for the names it holds. A graph that cannot be read reads nothing: it cannot run either.
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
			var v any
			if json.Unmarshal(raw, &v) == nil && f.readsValue(v) {
				return true
			}
		}
	}
	return false
}

func (f Files) readsValue(v any) bool {
	switch v := v.(type) {
	case string:
		return f.readsString(v)
	case []any:
		for _, e := range v {
			if f.readsValue(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range v {
			if f.readsValue(e) {
				return true
			}
		}
	}
	return false
}

// readsString judges one string leaf: a file name (the exact match), else a text that holds the name of an adult file, with
// or without its extension, as a word of its own ("<lora:Spicy:0.8>" holds "Spicy", "Spicy.safetensors " holds the file).
func (f Files) readsString(s string) bool {
	if f[key(s)] {
		return true
	}
	text := strings.ToLower(s)
	for name := range f {
		if holdsWord(text, name) || holdsWord(text, stem(name)) {
			return true
		}
	}
	return false
}

// holdsWord reports whether text holds word between characters that cannot be part of a file name (the ends of the text
// count): "spicy" is in "<lora:spicy:0.8>" and in "spicy food" but not in "spicyfood" nor in "unspicy".
func holdsWord(text, word string) bool {
	if word == "" {
		return false
	}
	for from := 0; ; {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(word)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !isNameRune(before) && !isNameRune(after) {
			return true
		}
		from = start + 1
	}
}

// stem is the name of a file without its model extension ("spicy.safetensors" is "spicy"; "v2.1" stays).
func stem(name string) string {
	switch ext := path.Ext(name); ext {
	case ".safetensors", ".sft", ".ckpt", ".pt", ".pth", ".bin", ".gguf":
		return strings.TrimSuffix(name, ext)
	}
	return name
}

// isNameRune reports whether r continues a name: a letter, a digit, "_" or "-" ("." is a separator: a sentence ends with
// it). The RuneError of the end of the text is none of these.
func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// key is the name of a file below its folder, in lower case ("sub\\Name.safetensors" is "name.safetensors").
func key(name string) string {
	return strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
}
