// Package local is the "ローカル・代替モデル" category of the global settings screen (spec/components/settings-screen-categories.md
// §5): the card of the model manager, the ComfyUI and OpenAI-compatible endpoints the user registered as cards, the modal
// that adds or changes one (with its connection test) and the page of an endpoint with its models
// (spec/architecture/local-models.md). A save answers with the card or row it changed, out of band; the connection test
// answers with a result line and the API key form with its own row. The presentation layer only knows what the handler
// hands it: nothing here imports the service layer.
package local

import "strconv"

// Kinds of an endpoint and the two media of a model, as the forms carry them.
const (
	KindComfyUI      = "comfyui"
	KindOpenAICompat = "openai-compat"
	MediaImage       = "image"
	MediaVideo       = "video"
)

// MaxNameLen is the longest endpoint or model name (localcfg.ValidName); NamePattern is what it may be made of.
const (
	MaxURLLen     = 2048
	MaxModelIDLen = 200 // the model identifier of the server (localcfg: at most 200 characters)
	MaxNameLen    = 63
	NamePattern   = "[a-z0-9][a-z0-9._-]*"
)

// Names of the fields of the forms (the handler reads the same names).
const (
	FieldName         = "name"
	FieldKind         = "kind"
	FieldBaseURL      = "base_url"
	FieldConcurrency  = "max_concurrency"
	FieldTimeout      = "timeout_seconds"
	FieldAPIKey       = "api_key"
	FieldMedia        = "media"
	FieldTemplate     = "template"
	FieldWorkflowText = "workflow_json"
	FieldWorkflowFile = "workflow_file"
	FieldGraph        = "graph"
	FieldOutputNode   = "output_node"
	FieldFPS          = "fps"
	FieldFramesStep   = "frames_step"
	FieldSizeMultiple = "size_multiple"
	FieldDurations    = "durations"
	FieldResolutions  = "resolutions"
	FieldNativeAudio  = "native_audio"
	FieldModelID      = "model_id"
	FieldAcceptsImage = "accepts_image_inputs"
	FieldCostImage    = "cost_per_image_usd"
	FieldCostSecond   = "cost_per_second_usd"
)

// AssignField is the name of the <select> that says which node input receives the value param. A reference
// image has one slot per image (index); the other parameters have one (index 0).
func AssignField(param string, index int) string {
	if param != "reference_image" {
		return "assign_" + param
	}
	return "assign_" + param + "_" + strconv.Itoa(index)
}

// ReferenceSlots is how many reference image rows the table of a workflow has at least: a workflow with more references (the
// stored model of a bundled editing workflow takes ten) shows a row for each.
const ReferenceSlots = 4

// Data is the category and the page of an endpoint: the saved endpoints with their models and, on the page of an endpoint,
// the model form an action opened. Pinned means the file was fixed by --model-config: it is shown, not changed (the API key
// of an endpoint and the connection test still work) and PinnedNote says why. Problem: the settings file cannot be read.
// Templates are the built-in workflows a ComfyUI model starts from.
type Data struct {
	Pinned     bool
	PinnedNote string
	Problem    string
	Endpoints  []Endpoint
	Templates  []TemplateOption
	Editor     Editor
}

// Endpoint is a registered endpoint. It never carries the API key, only whether there is one and its end.
type Endpoint struct {
	Name           string
	Kind           string
	KindLabel      string
	BaseURL        string
	MaxConcurrency int
	TimeoutSeconds int
	Key            Key
	Models         []Model
	// NoPage is an endpoint whose name is the address of the form that adds one ("new"): it has no page of its own, so
	// its models cannot be opened (a name the service refuses now, kept in a file saved before). It is deleted and made again.
	NoPage bool
}

// Key is the API key of an endpoint as the screen shows it: set or not, and its last characters.
type Key struct {
	Configured bool
	LastChars  string
	// Unavailable: the OS key store could not be read, so the state of the key is unknown.
	Unavailable bool
	// Error is why the last attempt to save the key was refused (set by the key form only).
	Error string
}

// Model is a model defined on an endpoint. ID is what take.model_name holds. Summary is its capabilities in
// a line. UsedBy lists where it is chosen: such a model cannot be deleted.
type Model struct {
	Name    string
	ID      string
	Media   string
	Summary string
	UsedBy  []string
}

// TemplateOption is a built-in workflow.
type TemplateOption struct {
	ID          string
	Label       string
	Media       string
	Description string
}

// Editor is the form of a model that an action opened on the page of an endpoint. At most one of its fields is set.
type Editor struct {
	Start *ModelStart
	Model *ModelForm
	// Error is why the form was refused; it is shown inside the form.
	Error string
}

// Open reports whether a form is open.
func (e Editor) Open() bool { return e.Start != nil || e.Model != nil }

// EndpointForm is the body of the modal that adds an endpoint or, with Edit, changes the address, concurrency and timeout
// of one: the name and the kind are fixed for good (they are in the identifiers of Takes and the Cost Ledger), so an
// edit shows them and does not offer them as fields. OnPage: the modal was opened on the page of the endpoint, whose
// summary a save replaces (a save on the list replaces the card of the list).
type EndpointForm struct {
	Edit           bool
	OnPage         bool
	Name           string
	Kind           string
	BaseURL        string
	MaxConcurrency string
	TimeoutSeconds string
}

// ModelStart is the first step of a ComfyUI model: a template, or a workflow to import (with its media).
type ModelStart struct {
	Endpoint     string
	Media        string
	TemplateID   string
	WorkflowText string
}

// NodeChoice is a node of the workflow with the inputs a value can be put into (the ones no other node feeds).
type NodeChoice struct {
	ID      string
	Class   string
	Options []InputChoice
}

// InputChoice is one input of a node: Value is "<node>|<input>", Label the input with its current value.
type InputChoice struct {
	Value string
	Label string
}

// Slot is one value of mam (prompt, seed, a reference image, ...) with the node input it goes to
// ("" for none). Param and Index (the number of a reference image) say which value it is.
type Slot struct {
	Param    string
	Index    int
	Field    string
	Label    string
	Selected string
}

// ModelForm defines (or, with Edit, changes) a model. Endpoint, EndpointKind and Name are fixed: an edit
// shows the name and does not offer it as a field. A ComfyUI model has Graph, Slots, Nodes and the output
// node; an OpenAI-compatible one has ModelID. Video models have the video fields.
type ModelForm struct {
	Edit         bool
	Endpoint     string
	EndpointKind string
	Name         string
	Media        string
	// Source is the ID of the template a new model started from (a hidden field): the template's reference mode, LoRA node and
	// the like are not fields of the form and are taken from it on save. Empty for an imported workflow and for an edit.
	Source string

	ModelID       string
	AcceptsImages bool

	Graph      string
	Nodes      []NodeChoice
	Slots      []Slot
	OutputNode string

	FPS          string
	FramesStep   string
	SizeMultiple string
	Durations    string
	Resolutions  string
	NativeAudio  bool

	CostPerImage  string
	CostPerSecond string
}

// TestView is the outcome of a connection test.
type TestView struct {
	OK      bool
	Message string
	Note    string
	Models  []string
}
