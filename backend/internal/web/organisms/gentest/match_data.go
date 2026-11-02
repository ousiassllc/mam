package gentest

import (
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// DOM ids and names of the prompt match screen (#916).
const (
	AdjustID = "gt-match-adjust"
	// MatchFieldNames are the form fields the handlers read.
	FieldMatchMode        = "mode"
	FieldMatchTarget      = "target_score"
	FieldMatchRounds      = "max_rounds"
	FieldMatchMaxCost     = "max_cost_usd"
	FieldMatchInstruction = "instruction"
	FieldMatchContinue    = "continue_batch_id"
)

// The ways to go on, as the form names them (the service's match.Mode*).
const (
	MatchAuto   = "auto"
	MatchOnce   = "once"
	MatchAdjust = "adjust"
)

// MatchMode is one of the three ways to go on.
type MatchMode struct {
	Value string
	Label string
	Hint  string
}

// MatchModes are the choices of the form, in order.
var MatchModes = []MatchMode{
	{MatchAuto, "おまかせ", "似るまで自動で直して作り直します"},
	{MatchOnce, "1回だけ", "プロンプトを作って 1 回だけ生成します"},
	{MatchAdjust, "調整", "1 回ずつ、結果を見ながら直します"},
}

// MatchModel is an image model the match form offers.
type MatchModel struct {
	Answer   commercial.View // the license of the model (#928)
	ID       string
	Label    string
	Price    string
	Selected bool
}

// MatchCompose is the form that starts a prompt match.
type MatchCompose struct {
	Message
	Models []MatchModel
	// Filter is the commercial-use filter of the model choices (#928), checked at the setting's default.
	Filter commercial.FilterData
	Mode   string
	// Target and Rounds are the numbers of the fields; MaxRounds is the most the service takes.
	Target, Rounds string
	// MaxCost is the field "上限の費用" ("" for none).
	MaxCost     string
	MaxRounds   int
	Instruction string
	// Ref is the reference image chosen without a file (from a link of the history): Value is the form field, ThumbURL its picture.
	RefValue, RefThumb string
	MaxImageBytes      int
	MaxInstruction     int
	MaxCostUSD         string
}

// MatchRound is a round of the match as the timeline draws it.
type MatchRound struct {
	N         int
	ID        string
	ImageURL  string
	DetailURL string
	// Status is the status of the picture; StatusText its words.
	Status     string
	StatusText string
	// Score is the similarity in words ("72%"; "" while it is not measured), Percent the same as a number for the bar.
	Score   string
	Percent int
	Reached bool
	Best    bool
	Active  bool
	// Change, Instruction and Differences are what the round learned and what was fixed; Prompt is the English prompt of the round
	// and BasePrompt the one it was fixed from.
	Change      string
	Instruction string
	Differences []string
	Prompt      string
	BasePrompt  string
	Error       string
	BasedOn     int
}

// MatchBatch is a prompt match as the screen shows it.
type MatchBatch struct {
	Message
	ID        string
	StateText string
	// StateTone is the badge: succeeded, failed, running or neutral.
	StateTone   string
	Finished    bool
	Model       string          // the label of the model the rounds were made with
	Answer      commercial.View // its license (#928)
	Reference   string          // the URL of the reference image ("" when it is not known)
	Target      int
	Done, Total int
	Percent     int
	Best        string // "72%" of the best round ("" when none was measured)
	BestPrompt  string
	BestUseURL  string
	Rounds      []MatchRound
	Estimate    string
	StopText    string
}

// MatchWordsForm is the words field below the match: while the match runs the words go into its next fix, after it they ask for
// one more round (which is estimated and confirmed first).
type MatchWordsForm struct {
	BatchID        string
	MaxInstruction int
	// OOB says the form is drawn out of band, into the place the page holds for it, with the box of a match that just started.
	OOB bool
}

func (b MatchBatch) pollURL() string { return paths.GenerationTestPart("match", "batches", b.ID) }
