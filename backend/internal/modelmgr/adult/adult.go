// Package adult judges what an adult model is: the names of the models the model manager created for it, and the files it
// reads (spec/architecture/model-manager/model-manager.md §6.1). The age gate asks it for every model of the bundled endpoint
// and for every other model whose workflow reads one of those files, so that a model that was renamed, an imported graph, a
// saved workflow or a model on another ComfyUI endpoint that reads the same file is judged as the adult model it is.
// It depends on the catalog and the record of the model manager only.
package adult

import (
	"encoding/json"
	"path"
	"regexp"
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
// well) that names it. Every value is walked down to its string leaves and the keys of its objects, so a file that sits in
// an object (a rgthree Power Lora Loader), a list or the key of a map is read too, and a leaf that is not a bare file name
// is read for the names it holds (see readsString). A graph that cannot be read reads nothing: it cannot run either.
func (f Files) Reads(graph json.RawMessage) bool {
	if len(f) == 0 || len(graph) == 0 {
		return false
	}
	g, err := localcfg.ParseGraph(graph)
	if err != nil {
		return false
	}
	r := newReader(f)
	for _, node := range g {
		for _, raw := range node.Inputs {
			var v any
			if json.Unmarshal(raw, &v) == nil && r.readsValue(v, 0) {
				return true
			}
		}
	}
	return false
}

// maxJSONDepth is how many times a string that holds JSON is decoded inside a string that holds JSON (a stringified LoRA
// stack in a stringified list). A deeper one is read as text only.
const maxJSONDepth = 4

// loraTag is the "<lora:NAME:weight>" tag of a prompt ("<lyco:...>" of LyCORIS too); the name is up to ":" or ">".
var loraTag = regexp.MustCompile(`<(?:lora|lyco):([^:>]+)`)

// reader judges the leaves of one graph against the files. The index is built once per Reads call, never kept.
type reader struct {
	files Files
	stems map[string]bool // the names of the files without their model extension: what a LoRA tag names
	odd   []string        // the file names that the tokens of a text cannot hold (a space, a bracket): scanned for
}

func newReader(f Files) reader {
	r := reader{files: f, stems: map[string]bool{}}
	for name := range f {
		if s := stem(name); s != name {
			r.stems[s] = true
		}
		if !isOneWord(name) {
			r.odd = append(r.odd, name)
		}
	}
	return r
}

func (r reader) readsValue(v any, depth int) bool {
	switch v := v.(type) {
	case string:
		return r.readsString(v, depth)
	case []any:
		for _, e := range v {
			if r.readsValue(e, depth) {
				return true
			}
		}
	case map[string]any:
		for k, e := range v {
			if r.readsString(k, depth) || r.readsValue(e, depth) {
				return true
			}
		}
	}
	return false
}

// readsString judges one string leaf (or key). It reads the file when the leaf is: the name of the file (the exact match,
// below any folder); a text that holds the full name of the file, with its extension, as a word of its own ("Spicy.safetensors "
// or "a, Spicy.safetensors."); the name of a LoRA tag, below any folder, with or without the extension ("<lora:Spicy:0.8>").
// A stem is never read as a free word of a text: "ae" or "model" are in any prompt. A "." that is between two name
// characters is part of the name ("spicy.v2" is not "spicy"). A leaf that is JSON is decoded and read again.
func (r reader) readsString(s string, depth int) bool {
	if r.files[key(s)] {
		return true
	}
	text := strings.ToLower(s)
	if words(text, func(w string) bool { return r.files[w] }) {
		return true
	}
	for _, name := range r.odd {
		if holdsWord(text, name) {
			return true
		}
	}
	if strings.Contains(text, "<") {
		for _, m := range loraTag.FindAllStringSubmatch(text, -1) {
			if k := key(strings.TrimSpace(m[1])); r.files[k] || r.stems[k] {
				return true
			}
		}
	}
	if depth < maxJSONDepth {
		if t := strings.TrimSpace(s); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
			var v any
			return json.Unmarshal([]byte(t), &v) == nil && r.readsValue(v, depth+1)
		}
	}
	return false
}

// words calls yield with every name of text (the runs of name characters) until it reports true, and reports that.
func words(text string, yield func(string) bool) bool {
	start := -1
	for i, r := range text {
		if isNameRune(r) || (r == '.' && start >= 0 && startsName(text[i+1:])) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && yield(text[start:i]) {
			return true
		}
		start = -1
	}
	return start >= 0 && yield(text[start:])
}

// isOneWord reports whether name is a single name of words: it has nothing that splits it.
func isOneWord(name string) bool {
	n, whole := 0, false
	words(name, func(w string) bool { n, whole = n+1, w == name; return false })
	return n == 1 && whole
}

// holdsWord reports whether text holds word between characters that cannot continue a name (the ends of the text count):
// the way of words for a file name that has a space or a bracket in it.
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
		before, size := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		joinsBefore := isNameRune(before) || (before == '.' && size == 1 && continuesBefore(text[:start-1]))
		joinsAfter := isNameRune(after) || (after == '.' && startsName(text[end+1:]))
		if !joinsBefore && !joinsAfter {
			return true
		}
		from = start + 1
	}
}

// continuesBefore reports whether text ends with a name character: a "." after it is part of a name that goes on.
func continuesBefore(text string) bool {
	r, _ := utf8.DecodeLastRuneInString(text)
	return isNameRune(r)
}

// startsName reports whether text starts with a letter or a digit: a "." before it joins the names around it.
func startsName(text string) bool {
	r, _ := utf8.DecodeRuneInString(text)
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// stem is the name of a file without its model extension ("spicy.safetensors" is "spicy"; "v2.1" stays).
func stem(name string) string {
	switch ext := path.Ext(name); ext {
	case ".safetensors", ".sft", ".ckpt", ".pt", ".pth", ".bin", ".gguf":
		return strings.TrimSuffix(name, ext)
	}
	return name
}

// isNameRune reports whether r continues a name: a letter, a digit, "_" or "-". A "." does only between two of them (a
// sentence ends with it). The RuneError of the end of the text is none of these.
func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// key is the name of a file below its folder, in lower case ("sub\\Name.safetensors" is "name.safetensors").
func key(name string) string {
	return strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
}
