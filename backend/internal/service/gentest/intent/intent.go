// Package intent is the list of "やりたいこと" the easy tier of the generation test screen and the MCP's intent offer (Issue #977:
// 人物写真・アニメ・風景・同じ顔で): each names the models that suit it, in the order of mam's preference. It is data (intents.json, embedded
// in the binary) so that a new choice or a new model is an edit of one file; the model that is used is picked from the ones that are
// installed and allowed (service/gentest.Service.Recommend), never one that is not.
package intent

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed intents.json
var data []byte

// Candidate is a model an intent may use. Model is the name of the workflow the model manager creates when it installs a catalog model
// (the model ID is comfyui:bundled-comfyui/<Model>), so the choice is the same on every PC. ReferenceOnly is a model that is made for an input
// image (an img2img workflow): it is taken only when the request has one. Note is what the screen says when this model was taken.
type Candidate struct {
	Model         string `json:"model"`
	Note          string `json:"note,omitempty"`
	ReferenceOnly bool   `json:"reference_only,omitempty"`
}

// Intent is one choice. Candidates are tried in order. NeedsReferences says the intent is made from an input image (the same face):
// without one it is refused. NeedsRole is the role of an input image a model must take for the intent (face).
type Intent struct {
	ID              string      `json:"id"`
	Label           string      `json:"label"`
	Hint            string      `json:"hint"`
	NeedsReferences bool        `json:"needs_references,omitempty"`
	NeedsRole       string      `json:"needs_role,omitempty"`
	Candidates      []Candidate `json:"candidates"`
}

var (
	once sync.Once
	all  []Intent
)

// All lists the intents in the order of the screen. It panics when the embedded data is invalid: a build problem that the tests catch.
func All() []Intent {
	once.Do(func() {
		var file struct {
			Intents []Intent `json:"intents"`
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&file); err != nil {
			panic(fmt.Sprintf("intent: intents.json: %v", err))
		}
		all = file.Intents
	})
	return append([]Intent(nil), all...)
}

// Get returns the intent with the ID.
func Get(id string) (Intent, bool) {
	for _, i := range All() {
		if i.ID == id {
			return i, true
		}
	}
	return Intent{}, false
}

// IDs lists the IDs of the intents.
func IDs() []string {
	var out []string
	for _, i := range All() {
		out = append(out, i.ID)
	}
	return out
}
