package localcfg

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"sync"
)

//go:embed templates/image/*.json templates/image/extra/*.json templates/video/*.json templates/video/ltx25/*.json templates/video/uncensored/*.json templates/upscale/*.json
var templateFS embed.FS

// Template is a ready workflow with its input assignments, shipped in the binary as data (templates/image/*.json, templates/image/extra/*.json, templates/video/*.json, templates/video/ltx25/*.json, templates/video/uncensored/*.json; the few-step and pose templates added for the uncensored catalog models (SDXL Lightning, Krea 2 Turbo, Rapid-AIO, VNCCS PoseStudio) are in templates/image/extra, which keeps that directory under the line limit; the LTX-2.5 ones added with FLF2V, DFR and the prompt enhancer are in templates/video/ltx25, and the video templates of the uncensored models of Issue #1012 (Wan 2.1 1.3B, Wan 2.2 5B and 14B merges and LoRAs) are in templates/video/uncensored, which keeps the directories under the line limit; the workflows that enlarge a picture (Workflow.Upscale) are in templates/upscale/*.json):
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

// templateFiles lists the embedded template files (templates/image/extra, templates/video/ltx25 and templates/video/uncensored are listed on their own).
func templateFiles() []string {
	var files []string
	for _, dir := range []string{"templates/image", "templates/image/extra", "templates/video", "templates/video/ltx25", "templates/video/uncensored", "templates/upscale"} {
		entries, err := fs.ReadDir(templateFS, dir)
		if err != nil {
			panic(err) // the directory is embedded: a build problem, not a run-time one
		}
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, dir+"/"+e.Name())
			}
		}
	}
	return files
}

// readTemplate decodes one embedded template file into a template of its own (no caller shares what another changed).
func readTemplate(file string) Template {
	data, err := templateFS.ReadFile(file)
	if err != nil {
		panic(err)
	}
	var t Template
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		panic(fmt.Sprintf("template %s: %v", file, err))
	}
	return t
}

// Templates lists the built-in workflow templates: image (text to image, reference to image) and video
// (text to video, first frame to video), ordered by ID.
func Templates() []Template {
	var out []Template
	for _, file := range templateFiles() {
		out = append(out, readTemplate(file))
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

// fileOfTemplate is the embedded file of each template ID, read once: a lookup by ID then decodes one file, not all of them (the catalog
// validation looks up a template for every workflow of every model).
var fileOfTemplate = sync.OnceValue(func() map[string]string {
	index := map[string]string{}
	for _, file := range templateFiles() {
		if id := readTemplate(file).ID; index[id] == "" {
			index[id] = file
		}
	}
	return index
})

// TemplateByID returns the template with the ID.
func TemplateByID(id string) (Template, bool) {
	file, ok := fileOfTemplate()[id]
	if !ok {
		return Template{}, false
	}
	return readTemplate(file), true
}
