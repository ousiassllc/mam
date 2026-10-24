// Package workflows is the screens of the ComfyUI workflows saved in mam: the node screen (the ComfyUI node UI in an
// iframe, spec/architecture/comfy/comfy-embedding.md) and the list of saved workflows with their import, input assignment
// and warnings (spec/architecture/comfy/comfy-workflows.md). The presentation layer only knows what the handler hands it.
package workflows

import (
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/localmodels"
)

// IDs of the elements the actions swap.
const (
	PanelID  = "workflows-panel"
	EditorID = "workflows-editor"
)

// Names of the fields of the forms (the handler reads the same names).
const (
	FieldName         = "name"
	FieldKind         = "kind"
	FieldEndpoint     = "endpoint"
	FieldAPIJSON      = "api_json"
	FieldAPIFile      = "api_file"
	FieldRemotePath   = "remote_path"
	FieldReplace      = "replace"
	FieldOutputNode   = "output_node"
	FieldFPS          = "fps"
	FieldFramesStep   = "frames_step"
	FieldSizeMultiple = "size_multiple"
	FieldDurations    = "durations"
	FieldResolutions  = "resolutions"
	FieldNativeAudio  = "native_audio"
)

// MaxNameLen is the longest workflow name (service/workflows.MaxNameRunes).
const MaxNameLen = 100

// Kinds of a workflow.
const (
	KindImage = "image"
	KindVideo = "video"
)

// The types of the assignment editor are the model form's of the settings screen (the same table).
type (
	// Slot is one value of mam with the node input it goes to.
	Slot = localmodels.Slot
	// NodeChoice is a node of the graph with the inputs a value can go to.
	NodeChoice = localmodels.NodeChoice
)

// Row is a saved workflow in the list.
type Row struct {
	ID        string
	Name      string
	Kind      string
	Endpoint  string
	ModelID   string
	Version   int
	Updated   string
	Ready     bool
	Problems  []string
	HasUI     bool
	UsedBy    []string
	Warnings  []Warning
	CheckedOn string // the endpoint the warnings come from ("" when not checked)
	Checked   bool
	// Unreachable is why the check could not ask the ComfyUI ("" when it could).
	Unreachable string
}

// Warning is something the connected ComfyUI lacks for a workflow.
type Warning struct {
	Kind    string
	Message string
}

// RemoteFile is a workflow saved in the ComfyUI server's own workflows folder.
type RemoteFile struct {
	Path string
	Name string
}

// Editor is the form that changes a workflow: its name, endpoint, input assignment, output node and video settings.
type Editor struct {
	ID       string
	Name     string
	Kind     string
	Endpoint string
	ModelID  string
	Version  int
	Nodes    []NodeChoice
	Slots    []Slot
	Output   string

	FPS          string
	FramesStep   string
	SizeMultiple string
	Durations    string
	Resolutions  string
	NativeAudio  bool

	Endpoints []string
	Problems  []string
	Error     string
	// Versions are the versions that exist (newest first): a Take names "comfy:<id>@v<N>".
	Versions []VersionRow
}

// VersionRow is one saved version.
type VersionRow struct {
	Version int
	ModelID string
	Current bool
	At      string
}

// Open reports whether an editor is shown.
func (e *Editor) Open() bool { return e != nil && e.ID != "" }

// Data is the whole panel.
type Data struct {
	Problem   string
	Notice    string
	Error     string
	Workflows []Row
	Endpoints []string
	Editor    *Editor
	// Remote is the list of workflows the ComfyUI server holds (after "ComfyUI の一覧を読む"); RemoteEndpoint is the
	// endpoint it came from, RemoteError why it could not be read.
	Remote         []RemoteFile
	RemoteEndpoint string
	RemoteError    string
	// Form holds what the import forms were submitted with, so that a refusal does not lose it.
	FormName, FormKind, FormEndpoint, FormJSON string
}

func itoa(n int) string { return strconv.Itoa(n) }

func kindLabel(k string) string {
	if k == KindVideo {
		return "動画"
	}
	return "画像"
}
