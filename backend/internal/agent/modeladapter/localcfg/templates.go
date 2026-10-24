package localcfg

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
)

//go:embed templates/*.json
var templateFS embed.FS

// Template is a ready workflow with its input assignments, shipped in the binary as data (templates/*.json):
// adding one needs no code. It uses core ComfyUI nodes only, so the server needs no custom nodes. Only the
// model files (the checkpoint names inside the graph) have to be present on the server, and the names may
// need changing to what the server has.
type Template struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Media       Media  `json:"media"`
	Description string `json:"description"`
	// Model is the model a new entry of "local_models" starts from.
	Model Model `json:"model"`
}

// Templates lists the built-in workflow templates: image (text to image, reference to image) and video
// (text to video, first frame to video), ordered by ID.
func Templates() []Template {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		panic(err) // the directory is embedded: a build problem, not a run-time one
	}
	var out []Template
	for _, e := range entries {
		data, err := templateFS.ReadFile("templates/" + e.Name())
		if err != nil {
			panic(err)
		}
		var t Template
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&t); err != nil {
			panic(fmt.Sprintf("template %s: %v", e.Name(), err))
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Template) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	return out
}

// TemplateByID returns the template with the ID.
func TemplateByID(id string) (Template, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}
