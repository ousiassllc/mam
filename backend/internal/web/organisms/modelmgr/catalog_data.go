package modelmgr

// File is a file of a model.
type File struct {
	Path    string
	Size    string
	Present bool
	Shared  []string
	// SourceNote is where the original of a third party's mirror lies ("" for a file of the original repository).
	SourceNote string
}

// Model is a model of the catalog.
type Model struct {
	ID        string
	Name      string
	KindLabel string
	Advanced  bool
	// Adult is the badge 成人向け; TriggerWords are the words a LoRA of the model needs in the prompt.
	Adult        bool
	TriggerWords []string
	// Sources are the places of the originals of the mirrored files (each once).
	Sources     []string
	Description string
	Size        string
	Need        string
	// LowDisk: what the install still has to download does not fit in the free space.
	LowDisk    bool
	VRAM       string
	NoSource   bool
	License    License
	Consent    bool
	Accepted   bool
	NeedsToken bool
	// State is not_installed, installing, installed or incomplete.
	State         string
	Install       *Progress
	Files         []File
	ModelIDs      []string
	WorkflowCheck string
}

// License is what the screen shows of the license of a model's weights.
type License struct {
	ID         string
	Version    string
	Name       string
	URL        string
	Summary    string
	Commercial string
	Regions    string
	Notice     string
}

// Catalog is the list of models with the state of each.
type Catalog struct {
	Message
	Models []Model
	// Jobs are the install jobs the list reports on (running, or just ended).
	Jobs []string
	// OOB draws the list out of band (the answer of another section's action: what can be installed changed).
	OOB bool
	// Blocked says why nothing can be installed now ("" when something can): no engine, an OS without it, a pinned file.
	Blocked string
}

// Polling reports whether an install is running (or has not been seen to end yet).
func (c Catalog) Polling() bool { return len(c.Jobs) > 0 }

// Consent is the license dialog of a model that needs an acceptance.
type Consent struct {
	ModelID string
	Name    string
	License License
	// AdultNotice is the notice for adult models, shown after the license summary ("" for a general model); Sources are the
	// places of the originals of the mirrored files.
	AdultNotice string
	Sources     []string
	Error       string
}

// Adult is the setting "成人向けモデルを表示する".
type Adult struct {
	Message
	Enabled bool
	// Confirmed: the age is confirmed in the current wording, so the setting turns on without asking again.
	Confirmed   bool
	ConfirmedAt string
}

// AdultConfirm is the dialog that asks the age confirmation: the notice and its version, which the form sends back.
type AdultConfirm struct {
	Notice        string
	NoticeVersion string
	Error         string
}
