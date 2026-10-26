package localcfg

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync/atomic"
)

// WorkflowSpec is what a saved workflow stores beside its graph (comfy_workflow.assignment_json): the table that
// tells mam where its values go, the output node, the frame settings and, for a video workflow, the clip lengths and
// sizes it can produce. The graph itself is stored apart (API format).
type WorkflowSpec struct {
	Assignments  []Assignment   `json:"assignments"`
	OutputNode   string         `json:"output_node"`
	FPS          int            `json:"fps,omitempty"`
	FramesStep   int            `json:"frames_step,omitempty"`
	SizeMultiple int            `json:"size_multiple,omitempty"`
	Video        *VideoSettings `json:"video,omitempty"`
}

// DefaultVideoSettings are the lengths and sizes a video workflow gets until the user sets them (the Wan 2.2 5B range
// of the built-in templates).
func DefaultVideoSettings() VideoSettings {
	return VideoSettings{Durations: []float64{3, 4, 5}, Resolutions: []string{"480p", "720p"}}
}

// ParseSpec reads assignment_json; an empty value is an empty spec.
func ParseSpec(raw []byte) (WorkflowSpec, error) {
	var s WorkflowSpec
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return WorkflowSpec{}, fmt.Errorf("the workflow settings cannot be read: %w", err)
	}
	return s, nil
}

// Model is the model a saved workflow of the media stands for, with the graph in API format. A video workflow
// without settings gets DefaultVideoSettings. The price is 0: a local run has no vendor bill, and the Cost Ledger
// still gets its row.
func (s WorkflowSpec) Model(media Media, graph json.RawMessage) Model {
	m := Model{Media: media, Workflow: &Workflow{
		Graph: graph, Assignments: slices.Clone(s.Assignments), OutputNode: s.OutputNode,
		FPS: s.FPS, FramesStep: s.FramesStep, SizeMultiple: s.SizeMultiple,
	}}
	if media == MediaVideo {
		v := DefaultVideoSettings()
		if s.Video != nil {
			v = *s.Video
		}
		m.Video = &v
	}
	return m
}

// ValidateSaved checks a saved workflow's model the way a registered model is checked (the graph, the assignments,
// the output node, the video settings). A nil result means it can run; the messages are English and name no endpoint.
func ValidateSaved(m Model) []error {
	if m.Workflow == nil || (m.Media != MediaImage && m.Media != MediaVideo) {
		return []error{fmt.Errorf("the workflow has no graph or no kind")}
	}
	errs := validateWorkflow("workflow", m.Media, *m.Workflow)
	if m.Media == MediaVideo {
		errs = append(errs, validateVideoSettings("workflow", m.Video)...)
	}
	return errs
}

// SavedWorkflow is one version of a workflow saved in mam, as a model of a ComfyUI endpoint. It is identified
// ("comfy:<WorkflowID>@v<Version>", modelreg.WorkflowID) in take.model_name and cost_ledger_entry.provider.
type SavedWorkflow struct {
	// ID is the identifier of this version.
	ID         string
	WorkflowID string
	Version    int
	Name       string
	// Endpoint is the registered ComfyUI endpoint that runs the workflow.
	Endpoint string
	// Current is true for the latest version: the one offered for selection.
	Current bool
	// Pinned: a Shot's Video Model lock names this version, so it is offered although it is not the current one.
	Pinned bool
	Model  Model
}

var savedWorkflows atomic.Pointer[map[string]SavedWorkflow]

// PublishWorkflows replaces the saved workflows (every version that can run, by identifier) that configcheck.Load
// attaches to the settings it reads: the workflows live in the database, not in the settings file, yet the routes of
// the file may name them. The workflows service publishes them whenever they change, before the settings are read
// again. The application has one set; tests that publish must restore it (PublishWorkflows(nil)).
func PublishWorkflows(w map[string]SavedWorkflow) {
	c := maps.Clone(w)
	savedWorkflows.Store(&c)
}

// PublishedWorkflows are the workflows PublishWorkflows last published (nil when none).
func PublishedWorkflows() map[string]SavedWorkflow {
	if p := savedWorkflows.Load(); p != nil {
		return *p
	}
	return nil
}

// Latest is the identifier of the current version of the workflow the identifier names, or id itself when it names
// no published workflow. A route of the settings means "use this workflow", so it follows the latest version; a Shot's
// Video Model lock pins an exact one.
func Latest(id string) string {
	w, ok := PublishedWorkflows()[id]
	if !ok || w.Current {
		return id
	}
	for cur, c := range PublishedWorkflows() {
		if c.Current && c.WorkflowID == w.WorkflowID {
			return cur
		}
	}
	return id
}

// EndpointOf is the endpoint a model identifier runs on: the one a published saved workflow names, or the one of a
// "<kind>:<endpoint>/<name>" identifier.
func EndpointOf(id string) (string, bool) {
	if w, ok := PublishedWorkflows()[id]; ok {
		return w.Endpoint, true
	}
	_, endpoint, _, ok := ParseID(id)
	return endpoint, ok
}
