// Package modelmgr is the model manager screen (paths.ModelManager): the bundled ComfyUI engine, the catalog of local
// image and video models, the Hugging Face token, the view of an external ComfyUI, and the trial generations
// (spec/architecture/model-manager/model-manager.md, spec/api/endpoints-model-manager.md). Every action answers with the
// section it changed (outerHTML of its element); a section that has a job running polls itself. The presentation
// layer only knows what the handler hands it: nothing here imports the service layer, and no value here is a key or
// a token.
package modelmgr

import (
	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// IDs of the elements the actions swap.
const (
	EngineID     = "mm-engine"
	GPUID        = "mm-gpu"
	DetectID     = "mm-detect"
	CatalogID    = "mm-catalog"
	DialogID     = "mm-dialog"
	HFID         = "mm-hf"
	ExternalID   = "mm-external"
	TrialID      = "mm-trial"
	AdultID      = "mm-adult"
	CommercialID = "mm-commercial"
	ModelsID     = "mm-models"
	TrialFmID    = "mm-trial-form"
	TrialsID     = "mm-trials"
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
	Engine Engine
	Adult  Adult
	// Commercial is the setting "商用OKのモデルだけを使う" with the filter of the model list (#928).
	Commercial CommercialPanel
	Catalog    Catalog
	HF         HFToken
	External   External
	Trial      TrialPanel
	HFImport   HFImport
	// Runtimes is the section of the local voice and lip-sync runtimes (runtimecard.RuntimesSection); nil leaves the section out.
	Runtimes templ.Component
	// Nodes is the section of the custom nodes (nodecard.NodesSection); nil leaves the section out.
	Nodes templ.Component
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
	// UpdatedAt is the time of day it was read ("15:04:05"), beside the refresh button (#962).
	UpdatedAt string
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

// engineHelp is the help of the engine section: what the card does not say (the modal it opens, the license, the autostart, the removal).
const engineHelp = "画像・動画のモデルを動かす ComfyUI を、mam が取得して起動・停止します。インストーラーには含まれず、ここから導入します。\n" +
	"カードをタップすると、版・待ち受け・導入先・出典・ライセンスが見られます。ComfyUI（GPL-3.0）は別のプログラムで、mam には含まれません。公式のリリースを実行時に取得し、SHA-256 を確かめてから使います。\n" +
	"「自動起動」をオンにすると、mam の起動時に自動で起動します（オフのときは、最初に使うときに起動）。削除すると導入先も一緒に消えます（導入したモデルのファイルは残ります）。導入は固定した版を取得し、SHA-256 が合わなければ破棄します。中断しても続きから再開できます。\n" +
	"同梱の ComfyUI は Windows 版のデスクトップアプリだけで使えます。ブラウザ版・サーバー版では、別に動かしている ComfyUI の URL を設定の「ローカル・代替モデル」に登録してください。"

// EngineName is the name of the engine card (the title of its modal and the subject of its buttons).
const EngineName = "ComfyUI エンジン"

// EnginePhase is where the engine is, in one word (the data-engine of the card): unsupported, readonly, installing, broken, missing or installed.
func EnginePhase(e Engine) string {
	switch {
	case !e.Supported:
		return "unsupported"
	case !e.Writable:
		return "readonly"
	case e.Install != nil:
		return "installing"
	case e.Broken:
		return "broken"
	case !e.Installed:
		return "missing"
	}
	return "installed"
}

var phaseBadges = map[string]struct {
	tone  atoms.BadgeTone
	label string
}{
	"unsupported": {atoms.BadgeNeutral, "この PC では使えません"}, "readonly": {atoms.BadgeError, "導入先に書き込めません"},
	"installing": {atoms.BadgeInfo, "導入中"}, "broken": {atoms.BadgeWarn, "ファイルが見つかりません"}, "missing": {atoms.BadgeNeutral, "未導入"},
}

// EngineBadge is the one badge that sums the card up (also the one of its modal): the phase, and once installed the state of the process.
func EngineBadge(e Engine) (atoms.BadgeTone, string) {
	if b, ok := phaseBadges[EnginePhase(e)]; ok {
		return b.tone, b.label
	}
	return stateBadge(e.State)
}

// engineCard is what the card says (modelcard.Card): the version and build under the name once installed. A card that polls carries the request
// on its <li>, as the runtimes do; the one that opens the modal is its name.
func engineCard(e Engine) modelcard.Card {
	tone, label := EngineBadge(e)
	phase, meta := EnginePhase(e), "画像・動画のモデルを動かすエンジン"
	attrs := templ.Attributes{"data-testid": "mm-engine", "data-engine": phase, "data-state": e.State}
	if e.Polling() {
		attrs["hx-get"], attrs["hx-trigger"], attrs["hx-swap"] = engineURL(e), Polling, "outerHTML"
	}
	c := modelcard.Card{ID: EngineID, Name: EngineName, Icon: atoms.IconWorkflow, DetailURL: paths.ModelManagerPart("engine", "detail"), Attrs: attrs,
		State: modelcard.Badge{Tone: tone, Label: label, Attrs: templ.Attributes{"data-testid": "mm-engine-state", "data-state": e.State}}}
	if phase == "installed" {
		meta = "ComfyUI " + e.Version + "・" + e.VariantLabel
		if e.CPU {
			meta += "・CPU 実行"
		}
		if e.UpdateAvailable {
			c.Badges = append(c.Badges, modelcard.Badge{Tone: atoms.BadgeInfo, Label: "更新あり"})
		}
	}
	c.Meta = meta
	return c
}
