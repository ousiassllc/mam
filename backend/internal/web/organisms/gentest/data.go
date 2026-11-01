// Package gentest draws the generation test screen (paths.GenerationTest, spec/architecture/gentest/gentest-screen.md): the
// compose form, the confirmation of an estimate, the progress and comparison grid of a batch, the history, the result detail
// with its import form, and the prompt history. The handlers build these structs; nothing here calls a service.
package gentest

import (
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
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
	MaxNameLen   = domain.MaxReferenceNameRunes
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
}

// PromptText is a prompt and its negative prompt.
type PromptText struct {
	Text     string
	Negative string
}

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
	Checked  bool
}

// Compose is the form that starts a batch.
type Compose struct {
	Message
	Media  string // image or video
	Models []ModelChoice
	// Prompts has at least one entry.
	Prompts []PromptText
	// Translate is the switch "英語に訳して送る" (on for a fresh form).
	Translate                                    bool
	Count, Aspect, Resolution, Duration          string
	Resolutions, Durations                       []string
	MaxPrompt, MaxCount, MaxItems, MaxReferences int
	MaxCostUSD                                   string // "" when there is no cap
	Recent                                       []RecentPrompt
	Projects                                     []ProjectChoice
	Picked                                       []Ref
	Spend                                        string
}

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
	// Unknown notes the results whose price is not known and so are not in the total ("" when every price is known).
	Unknown string
	// OverLimit says why the batch cannot run ("" when it can); the confirm button is off then.
	OverLimit string
	Hidden    []Hidden
}

// Card is a result as a card of the grid, the history or the detail.
type Card struct {
	ID         string
	Model      string
	Prompt     string
	Status     string
	StatusText string
	Error      string
	FileURL    string
	Video      bool
	Favorite   bool
	Detail     string // the link to the detail
	Meta       string // "seed 123 ・ 16:9 ・ 2.1 MiB"
	CreatedAt  string
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
	Model string
	Cells [][]Card
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
}

// History is the history list.
type History struct {
	Message
	Filter HistoryFilter
	Models []string
	Cards  []Card
	Spend  string
	// Next is the link to the next page ("" on the last).
	Next string
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

// ImportForm is the form that imports a result into a Project.
type ImportForm struct {
	Message
	ResultID string
	Image    bool
	Projects []ProjectChoice
	// Done says the import was made: the form shows the outcome instead of the fields.
	Done string
}

// Detail is one result with everything about it.
type Detail struct {
	Message
	Card       Card
	SentPrompt string
	// English and EnglishNegative are what the prompt and the negative prompt were translated to before they were sent ("" when they
	// were sent as typed); the originals are the "入力したプロンプト" of Params.
	English         string
	EnglishNegative string
	Negative        string
	NegMode         string
	Params          []KV
	Inputs          []InputCard
	Imports         []ImportRow
	Import          ImportForm
	UseURL          string // compose with this result as an input ("" for a video)
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
