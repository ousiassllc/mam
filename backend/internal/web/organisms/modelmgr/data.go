// Package modelmgr is the model manager screen (paths.ModelManager): the bundled ComfyUI engine, the catalog of local
// image and video models, the Hugging Face token and the view of an external ComfyUI
// (spec/architecture/model-manager/model-manager.md, spec/api/endpoints-model-manager.md). Every action answers with the
// section it changed (outerHTML of its element); a section that has a job running polls itself. The presentation
// layer only knows what the handler hands it: nothing here imports the service layer, and no value here is a key or
// a token.
package modelmgr

import (
	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/companion"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/failbox"
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
	NodesID      = "mm-nodes" // the section of the custom nodes (nodecard.SectionID): the link of a model that needs a node
	AdultID      = "mm-adult"
	CommercialID = "mm-commercial"
	ModelsID     = "mm-models"
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
)

// MaxTokenLen is the limit of the token field (what the service accepts).
const MaxTokenLen = 512

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
	HFImport   HFImport
	// EngineNeeded says the bundled engine is not installed or not running: the page of the models says so above the list (an action of the engine
	// is on another page: the list is not drawn again from there).
	EngineNeeded bool
	// Runtimes is the section of the local voice and lip-sync runtimes (runtimecard.RuntimesSection); nil leaves the section out.
	Runtimes templ.Component
	// LLMUse, LLMRuntimes, LLMOllama and LLMConnection are the sections of the page "ローカル LLM" (#1104): the server the calls go to (llmcard.UseSection), the servers of mam
	// (runtimecard.RuntimesSection of the runtimes of a language model; nil when the app has no local runtimes), the models of its Ollama (ollamacard.OllamaSection, #1105) and "別のサーバー"
	// (llmcard.ConnectionSection). nil leaves a section out.
	LLMUse, LLMRuntimes, LLMOllama, LLMConnection templ.Component
	// Nodes is the section of the custom nodes (nodecard.NodesSection); nil leaves the section out.
	Nodes templ.Component
	// FaceModels is the section "顔チェック用モデル" (facecard.Section; #1026); nil leaves the section out.
	FaceModels templ.Component
	// Companion is the page of the companion software (the list, ffmpeg, FaceFusion; #1106); nil leaves it out.
	Companion *companion.Data
}

// Message is the confirmation or the refusal of the last action of a section.
// Failures are the installs that ended in a failure the job kept as the common error (#1010): each is drawn as an error box (failbox).
type Message struct {
	Notice   string
	Error    string
	Failures []failbox.Item
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
	// Supported is false off Windows and Apple silicon Macs: the engine does not exist there (use an external ComfyUI).
	Supported     bool
	Writable      bool
	WritableError string
	// Dir is where the engine and the models are (next to mam.exe on Windows, in the data directory on macOS); Free is the free space there.
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
	"カードをタップすると、版・待ち受け・導入先・出典・ライセンスが見られます。ComfyUI（GPL-3.0）は別のプログラムで、mam には含まれません。公式のリリース（Mac 版は、ソースと Python・PyTorch などの各パッケージを個別に）を実行時に取得し、SHA-256 を確かめてから使います。\n" +
	"「自動起動」をオンにすると、mam の起動時に自動で起動します（オフのときは、最初に使うときに起動）。削除すると導入先も一緒に消えます（導入したモデルのファイルは残ります）。導入は固定した版を取得し、SHA-256 が合わなければ破棄します。中断しても続きから再開できます。\n" +
	"同梱の ComfyUI は Windows 版と Apple Silicon の Mac（macOS 14 以降）版のデスクトップアプリだけで使えます。ブラウザ版・サーバー版や Intel Mac では、別に動かしている ComfyUI の URL を設定の「ローカル・代替モデル」に登録してください。"

// modelsHelpText is the help of the models section (the link to the generation test follows it, body.templ).
const modelsHelpText = "導入すると、画像・動画のモデルとして選べます（設定の「画像・動画モデル」・Shot の生成）。カードをタップすると、出典・ライセンス・ファイル一覧・SHA-256 が見られます。ダウンロードは取得元から直接で、SHA-256 を確かめます。ライセンスの同意が要るモデルは、同意してから導入します。導入先の空きが足りないときや、GPU のメモリが足りない見込みのときは、カードにそう出ます。成人向けモデル（非検閲のモデル・LoRA）は、18 歳以上の確認と注意書きへの同意の後に、一覧・導入・生成テストに出ます。"

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
