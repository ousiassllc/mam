// Package modelmgr is the model manager screen (paths.ModelManager): the bundled ComfyUI engine, the catalog of local
// image and video models, the Hugging Face token, the view of an external ComfyUI, and the trial generations
// (spec/architecture/model-manager/model-manager.md, spec/api/endpoints-model-manager.md). Every action answers with the
// section it changed (outerHTML of its element); a section that has a job running polls itself. The presentation
// layer only knows what the handler hands it: nothing here imports the service layer, and no value here is a key or
// a token.
package modelmgr

import "github.com/a-h/templ"

// IDs of the elements the actions swap.
const (
	EngineID   = "mm-engine"
	GPUID      = "mm-gpu"
	DetectID   = "mm-detect"
	CatalogID  = "mm-catalog"
	DialogID   = "mm-dialog"
	HFID       = "mm-hf"
	ExternalID = "mm-external"
	TrialID    = "mm-trial"
	AdultID    = "mm-adult"
	TrialFmID  = "mm-trial-form"
	TrialsID   = "mm-trials"
)

// Names of the form fields (the handler reads the same names).
const (
	FieldVariant   = "variant"
	FieldCPU       = "cpu"
	FieldOn        = "on"
	FieldToken     = "token"
	FieldLicenseID = "license_id"
	FieldLicenseV  = "license_version"
	FieldAgree     = "agree"
	FieldNoticeV   = "notice_version"
	FieldEndpoint  = "endpoint"
	FieldModelID   = "model_id"
	FieldPrompt    = "prompt"
	FieldNegative  = "negative_prompt"
	FieldAspect    = "aspect_ratio"
	FieldResol     = "resolution"
	FieldDuration  = "duration_seconds"
	FieldReference = "reference"
)

// Limits of the fields of the forms: the token (what the service accepts) and "16:9".
const (
	MaxTokenLen  = 512
	MaxAspectLen = 5
)

// Polling is how often a section with a running job asks for itself again.
const Polling = "every 2s"

// Data is the whole screen.
type Data struct {
	Engine   Engine
	Adult    Adult
	Catalog  Catalog
	HF       HFToken
	External External
	Trial    TrialPanel
	HFImport HFImport
	// Runtimes is the section of the local voice and lip-sync runtimes (runtimecard.RuntimesSection); nil leaves the section out.
	Runtimes templ.Component
}

// Message is the confirmation or the refusal of the last action of a section.
type Message struct {
	Notice string
	Error  string
}

// Progress is a running install: the job, how far it is and what it does.
type Progress struct {
	JobID   string
	Percent int
	Message string
	// Bytes is "1.2 GiB / 1.9 GiB" ("" when the step is not a transfer).
	Bytes string
}

// Engine is the card of the bundled ComfyUI engine.
type Engine struct {
	Message
	// Supported is false off Windows: the engine does not exist there (use an external ComfyUI).
	Supported     bool
	Writable      bool
	WritableError string
	// Dir is where the engine and the models are (next to mam.exe); Free is the free space there.
	Dir  string
	Free string

	Installed       bool
	Version         string
	PinnedVersion   string
	Variant         string
	VariantLabel    string
	CPU             bool
	UpdateAvailable bool
	Broken          bool

	// State is stopped, starting, running or failed.
	State        string
	URL          string
	ProcessError string
	AutoStart    bool

	Install *Progress
	// Job is the install job the card reports on (running, or one that just ended): a polling card carries it, so that how
	// the install ended is shown once it is over.
	Job         string
	SourceURL   string
	LicenseName string
	LicenseURL  string
}

// Polling reports whether the card has something in progress (an install, a start).
func (e Engine) Polling() bool { return e.Install != nil || e.State == "starting" }

// GPU is the detection result and the install form of the engine.
type GPU struct {
	Message
	Adapters []Adapter
	// Variant is the recommended build ("" for none); Reason says why and Guidance what to do without one.
	Variant      string
	Reason       string
	Confidence   string
	Guidance     string
	CPUAvailable bool
	Variants     []Variant
	Free         string
	// DetectError is why the adapters could not be listed.
	DetectError string
}

// Adapter is a GPU of the machine.
type Adapter struct {
	Name string
	VRAM string
}

// Variant is a build of the engine one can choose.
type Variant struct {
	Value       string
	Label       string
	Size        string
	Need        string
	Recommended bool
}

// DetectedComfy is a ComfyUI that answered on this machine.
type DetectedComfy struct {
	URL     string
	Version string
	Desktop bool
	Devices []string
}

// Detect is the result of "既存の ComfyUI を検出".
type Detect struct {
	Message
	Found       []DetectedComfy
	DesktopHint bool
	Recommended string
}
