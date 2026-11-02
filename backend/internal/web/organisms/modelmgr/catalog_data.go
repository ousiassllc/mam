package modelmgr

import (
	"net/url"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// File is a file of a model.
type File struct {
	Path string
	Size string
	// SHA256 is the digest the file is checked against at the install.
	SHA256  string
	Present bool
	Shared  []string
	// SourceNote is where the original of a third party's mirror lies ("" for a file of the original repository).
	SourceNote string
}

// Model is a model of the catalog.
type Model struct {
	ID   string
	Name string
	// Kind is image or video (the picture of the card); KindLabel says it in words.
	Kind      string
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
	LowDisk bool
	VRAM    string
	// VRAMFit is how the GPU of this PC stands to the guideline (catalog.Fit: short, limited, enough; "" when the memory is not known)
	// and VRAMFitText says it in words, with the limits of a GPU that is below the recommendation.
	VRAMFit     string
	VRAMFitText string
	NoSource    bool
	License     License
	Consent     bool
	Accepted    bool
	// NeedsToken is the badge 「HF トークンが必要」 (a model behind a Hugging Face login); TokenGuide is the guidance shown beside the
	// install while no token is stored ("" when there is one, or the model needs none).
	NeedsToken bool
	TokenGuide string
	// State is not_installed, installing, installed or incomplete.
	State         string
	Install       *Progress
	Files         []File
	ModelIDs      []string
	WorkflowCheck string
	// NeedsNodes says which custom nodes the model needs and does not have ("" when it needs none or has them): the install waits for them.
	NeedsNodes string
}

// License is what the screen shows of the license of a model's weights.
type License struct {
	ID         string
	Version    string
	Name       string
	URL        string
	Summary    string
	Commercial string
	// Answer is the commercial-use answer as the shared badge, note and filter draw it (#928).
	Answer  commercial.View
	Regions string
	Notice  string
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
	// UpdatedAt is the time of day it was read ("15:04:05"), beside the refresh button (#962).
	UpdatedAt string
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

// CommercialPanel is the setting "商用OKのモデルだけを使う" with the filter of the model list: the filter is drawn with the setting's
// answer checked, and the list keeps showing everything the filter lets through.
type CommercialPanel struct {
	Message
	// Only is the setting: the lists start at "商用OKのみ".
	Only   bool
	Filter commercial.FilterData
	// OOB draws the card out of band (the answer of another section's action).
	OOB bool
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

// joinNames lists names in a sentence.
func joinNames(names []string) string { return strings.Join(names, "、") }

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// engineURL is where the engine card asks for itself again: with the job it reports on.
func engineURL(e Engine) string {
	if e.Job == "" {
		return paths.ModelManagerPart("engine")
	}
	return paths.ModelManagerPart("engine") + "?job=" + url.QueryEscape(e.Job)
}

// catalogURL is where the model list asks for itself again: with the jobs it reports on.
func catalogURL(c Catalog) string {
	if len(c.Jobs) == 0 {
		return paths.ModelManagerPart("models")
	}
	return paths.ModelManagerPart("models") + "?jobs=" + url.QueryEscape(strings.Join(c.Jobs, ","))
}

// modelCardSelector is the CSS selector of the card of a model: where the focus goes when the dialog that was asked about it
// is gone and the card was drawn again.
func modelCardSelector(id string) string {
	return `[data-model="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(id) + `"]`
}

// FitClass colours the judgement of the GPU against a model's VRAM guideline: below the minimum is a danger, "動くが制約あり" a warning,
// and enough the plain text (color-rules.md §3.1).
func FitClass(fit string) string {
	switch fit {
	case "short":
		return "text-red-300"
	case "limited":
		return "text-amber-300"
	}
	return "text-neutral-300"
}

// NeedClass colours the free space an install needs: red when the disk cannot take it.
func NeedClass(low bool) string {
	if low {
		return "text-red-300"
	}
	return "text-neutral-300"
}
