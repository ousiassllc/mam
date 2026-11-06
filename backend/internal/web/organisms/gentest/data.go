// Package gentest draws the generation test screen (paths.GenerationTest, spec/architecture/gentest/gentest-screen.md): the
// compose form, the confirmation of an estimate, the progress and comparison grid of a batch, the history, the result detail
// with its import form, and the prompt history. The handlers build these structs; nothing here calls a service.
package gentest

import (
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/exportbar"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/faceswapbox"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/importbox"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/inputnotice"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/rolenotice"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/upscalebox"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/videoenhancebox"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestadv"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestresult"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// DOM ids the handlers' HTMX targets name.
const (
	ComposeID = "gt-compose"
	ConfirmID = "gt-confirm"
	BatchID   = "gt-batch"
	RefsID    = "gt-refs"
	PickerID  = "gt-picker"
	PromptsID = "gt-prompts"
	ListID    = "gt-list"
	// Polling is the hx-trigger of a section that asks for itself again while something runs.
	Polling = "every 2s"
)

// Form field names (the handlers read the same names).
const (
	FieldMedia      = "media"
	FieldModel      = "model"
	FieldPrompt     = "prompt"
	FieldNegative   = "negative"
	FieldTranslate  = "translate"
	FieldCount      = "count"
	FieldAspect     = "aspect_ratio"
	FieldResolution = "resolution"
	FieldDuration   = "duration_seconds"
	FieldRef        = "ref"
	FieldRolePrefix = "role_"
	FieldUpload     = "upload"
	FieldFirst      = "upload_first"
	FieldLast       = "upload_last"
	FieldEstimateID = "estimate_id"
	FieldTotal      = "confirmed_total_usd"
)

// Limits of the text fields (the service checks the same).
const (
	MaxAspectLen = len("99:99")
)

func itoa(n int) string { return strconv.Itoa(n) }

// Message is a notice (green) or an error (red) a section shows.
type Message struct {
	Notice string
	Error  string
}

// ModelChoice is a model the form offers.
type ModelChoice struct {
	ID       string
	Label    string
	Source   string // a short label of where it comes from (クラウド / ローカル / ワークフロー / ChatGPT)
	Price    string // "$0.04 / 枚", "無料（ローカル）", "単価なし"
	Features string // "シード対応・ネガティブ欄あり"
	// Reason is why the model cannot be chosen ("" when it can).
	Reason   string
	Selected bool
	// Media is image or video. RefMode ("edit", "img2img" or "" when it does not say), RefRoles (empty: the model takes no input
	// images) and CanLoRA are what the card's icon and the role, strength and LoRA controls follow.
	Media    string
	RefMode  string
	RefRoles []string
	CanLoRA  bool
	// RequiresFirstFrame: the model cannot run without a first frame (an I2V model). The card's data-requires-first-frame: the browser starts a picture as the first frame while one is ticked (#1009).
	RequiresFirstFrame bool
	Answer             commercial.View // whether the model may be used commercially (#928): the card's icon and note
	// Card is what every list of models says of the model the same way (#1011: handler/modelcards): the name, price, icon, badges and the reason it cannot be chosen.
	// Reason, above, is the one the form acts on (the model is not offered while it is set); the card's reason also carries where to fix it.
	Card                  modelcard.Card
	gentestadv.ModelFacts // the detailed settings the model takes and how it reads prompt weights (the card's data attributes)
}

// PromptText is a prompt and its negative prompt.
type PromptText = gentesttier.Prompt

// RecentPrompt is an entry of the prompt history offered for reuse.
type RecentPrompt struct {
	ID       string
	Text     string
	Negative string
	Uses     int
}

// ProjectChoice is a Project whose images can be picked.
type ProjectChoice struct {
	ID   string
	Name string
}

// Ref is an input image: a row of the list of chosen inputs or of a picker.
type Ref struct {
	// Value is "<source>:<project id>:<id>"; the form field ref carries it.
	Value    string
	Label    string
	ThumbURL string
	Role     string
	// Strength is how strongly an image model shows the reference (nil: not given, so the default; 0 is a strength).
	Strength *float64
}

// Compose is the form that starts a batch.
type Compose struct {
	Message
	Media  string // image or video
	Models []ModelChoice
	// Filter is the filter of the model cards: the bar, the panel and the chips (#1011).
	Filter modelpick.Filter
	// Prompts has at least one entry.
	Prompts []PromptText
	// Translate is the switch "英語に訳して送る" (on for a fresh form); Poses says the pose library is there: "ポーズを選ぶ" is offered (an image batch only).
	Translate, Poses bool
	// FaceOff says the face similarity is off now ("models_missing": the face models are not installed; "runtime_unavailable": the ONNX Runtime library is
	// missing; "" while it works, and for a video batch): the input images say so, with the link to the model manager's "顔チェック用モデル" (#1026).
	FaceOff                                      string
	Count, Aspect, Resolution, Duration          string
	Resolutions, Durations                       []string
	MaxPrompt, MaxCount, MaxItems, MaxReferences int
	MaxCostUSD                                   string // "" when there is no cap
	Recent                                       []RecentPrompt
	Projects                                     []ProjectChoice
	Picked                                       []Ref
	Spend                                        string
	MaxImageBytes                                int // the largest input image; the browser checks it when an image is chosen
	// AvoidPose is the checkbox "do not copy the pose of the references". LoRAs are the LoRA files of the engine (none: the choice
	// is not drawn), LoRA the one chosen ("" for none) and LoRAStrength its strength (nil: not given, so the default).
	AvoidPose    bool
	LoRAs        []string
	LoRA         string
	LoRAStrength *float64
	// SpeedModes are the speed modes the models offer (none: not drawn), SpeedMode the one chosen ("" for each model's default).
	SpeedModes         []SpeedChoice
	SpeedMode          string
	gentestadv.Section                   // the "詳細設定" and the "?" of the prompt weights
	InputCheck         inputnotice.Check // the notice of the input images (a video form; #1009)
	Tier               gentesttier.Data  // the tiers of an image form (かんたん / こだわり / ComfyUI; Issue #977)
}

// SpeedChoice is a speed mode of a model (localcfg.SpeedMode): the ID the form posts and the label of its option.
type SpeedChoice struct{ ID, Label string }

// Hidden is a form value the confirmation carries back unchanged.
type Hidden struct{ Name, Value string }

// ConfirmLine is one model of the estimate.
type ConfirmLine struct {
	Label    string
	Items    int
	Unit     string
	Subtotal string
}

// Confirm is the estimate the user confirms before a batch runs.
type Confirm struct {
	Message
	Items    int
	Total    string
	Lines    []ConfirmLine
	Warnings []string
	// InputWarnings are the warnings about input images a model leaves out (#1009): shown in a box of their own, above the table, so that they are not missed.
	InputWarnings []string
	// Unknown notes the results whose price is not known and so are not in the total ("" when every price is known).
	Unknown string
	// OverLimit says why the batch cannot run ("" when it can); the confirm button is off then.
	OverLimit string
	Hidden    []Hidden
	// Match says the estimate is of a prompt match (Items are its most rounds) and RunPath is where its confirmation is posted ("": the run of a batch).
	Match     bool
	RunPath   string
	RoleNotes []rolenotice.Note // models that use a reference as a whole picture (#959); the run button then opens their modal first.
}

func (c Confirm) runPath() string {
	if c.RunPath != "" {
		return c.RunPath
	}
	return paths.GenerationTestPart("run")
}

// Col is a column of the comparison grid: one prompt, and the English it was sent as when it was translated.
type Col struct {
	Text     string
	Negative string
	// English and EnglishNegative are the translation ("" when it was sent as typed).
	English         string
	EnglishNegative string
}

// Row is a row of the comparison grid: one model with the results of every prompt.
type Row struct {
	Model  string
	Answer commercial.View
	// Face is the face similarity of the results of this model read together ("" when the batch had no reference of the role "face", Issue #998).
	Face  string
	Cells [][]resulttile.Card
}

// Batch is the progress and the comparison grid of a batch.
type Batch struct {
	Message
	ID        string
	StateText string
	StateTone string
	Total     int
	Done      int
	Percent   int
	Counts    string // "成功 3・失敗 1・取り消し 0"
	Finished  bool
	Estimate  string
	Cols      []Col
	Rows      []Row
}

// HistoryFilter is the filter of the history as the form shows it.
type HistoryFilter struct {
	Model    string
	Media    string
	Favorite bool
	// Commercial is the commercial-use filter ("all", "ok", "ok_or_conditional"; "" for the default).
	Commercial string
}

// History is the history list.
type History struct {
	Message
	Filter HistoryFilter
	Models []string
	// Commercial is the commercial-use choice of the form (the server narrows the results by it).
	Commercial commercial.ChoiceData
	Cards      []resulttile.Card
	Spend      string
	// Next is the link to the next page ("" on the last).
	Next string
	// Refresh is the link to the list as it is now (the page, the filter) with "live=1": the part that the refresh button and the
	// polling ask for. Active says a result of the page is waiting or running, so the part asks for itself again every 2 s.
	// UpdatedAt is the time of day this was read ("15:04:05").
	Refresh   string
	Active    bool
	UpdatedAt string
	// Export is the bar that saves results as a ZIP.
	Export exportbar.Data
}

// KV is a line of the settings of a result.
type KV struct{ Key, Value string }

// InputCard is an input image of a batch.
type InputCard struct {
	Role   string
	Origin string
	URL    string
}

// ImportRow is where a result was imported to.
type ImportRow struct {
	ProjectName string
	ProjectURL  string
	What        string
	At          string
}

// Detail is one result with everything about it.
type Detail struct {
	Message
	Card       resulttile.Card
	SentPrompt string
	// English and EnglishNegative are the translation that was sent ("" when the text was sent as typed).
	English          string
	EnglishNegative  string
	Negative         string
	NegMode          string
	Params           []KV
	Inputs           []InputCard
	Unused           []inputnotice.Unused // the input images the model was not given, and why (#1009)
	Imports          []ImportRow
	Import           importbox.Form
	UseURL, MatchURL string                // set for an image that has a file
	Actions          gentestresult.Actions // the buttons under the picture (#977)
	Used             gentestresult.Used    // the settings it was made with, folded
	Upscale          upscalebox.Form       // "make it larger", and where an enlargement came from
	VideoEnhance     videoenhancebox.Form  // "make the video larger / smoother", and where an enhancement came from
	FaceSwap         faceswapbox.Form      // "swap the face" (FaceFusion), and where a swap came from
	Answer           commercial.View
	// CanPick says the detail was opened from a grid that has the choice for the ZIP export (the history, #932), so the modal offers it too.
	CanPick bool
}

// PromptRow is an entry of the prompt history.
type PromptRow struct {
	ID       string
	Text     string
	Negative string
	Uses     int
	LastUsed string
	UseURL   string
}

// Prompts is the prompt history.
type Prompts struct {
	Message
	Items []PromptRow
}
