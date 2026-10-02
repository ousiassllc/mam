package domain

import (
	"slices"
	"time"
)

// RepairDecider says who chose the strategy of a repair.
type RepairDecider string

// Deciders. A rule decides when the deterministic rules leave one candidate or prescribe the
// strategy (Face Identity drift); otherwise the Repair Agent (the Reasoning LLM) chooses.
const (
	DecidedByLLM  RepairDecider = "llm"
	DecidedByRule RepairDecider = "rule"
)

// RepairInstructions is the decision kept in repair_action.instructions.
type RepairInstructions struct {
	// Instructions are the snake_case repair instructions (from the Judge and the Repair Agent).
	Instructions []string      `json:"instructions,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	DecidedBy    RepairDecider `json:"decided_by"`
	// SourceTakeID is the Take that is repaired; it stays as it is.
	SourceTakeID string `json:"source_take_id"`
}

// RepairRecord is a row of repair_action: one repair of a Take by the Repair Agent. ResultingTakeID
// is the new Take it made ("" until it exists, and always for Edit-Level Repair, which makes no Take).
type RepairRecord struct {
	ID              string
	JudgeResultID   string
	Action          RepairAction
	Instructions    RepairInstructions
	ResultingTakeID string
	CreatedAt       time.Time
}

// Regenerates reports whether the action asks a video model for a new Take. Edit-Level Repair does
// not: it leaves the Take to the editor, so it is no retry of the Retry Policy.
func (a RepairAction) Regenerates() bool { return a.Valid() && a != RepairEditLevel }

// Label is the display name of the action.
func (a RepairAction) Label() string {
	switch a {
	case RepairPrompt:
		return "Prompt Repair"
	case RepairPartialRegeneration:
		return "Partial Regeneration"
	case RepairFullRetake:
		return "Full Retake"
	case RepairDifferentModel:
		return "Different Model"
	case RepairReferenceReinforce:
		return "Reference Reinforcement"
	case RepairEditLevel:
		return "Edit-Level Repair"
	}
	return string(a)
}

// RetriesDone is how many retries the Retry Policy counts in a Shot's repair history: the repairs
// that made a new Take. A repair that failed before it made a Take cost nothing, and Edit-Level
// Repair regenerates nothing. Takes of Prompt Candidate Generation count as well (CandidateRetries):
// a Shot's retries are RetriesDone(history) + CandidateRetries(candidate Takes of the Shot).
func RetriesDone(history []RepairRecord) int {
	n := 0
	for _, r := range history {
		if r.Action.Regenerates() && r.ResultingTakeID != "" {
			n++
		}
	}
	return n
}

// CandidateRetries is how many retries the Retry Policy counts for the Takes that Prompt Candidate
// Generation made: every one but the first is a retry, as a repair's new Take is. Both the candidate
// run (before it adds a Take) and the Repair loop (before it regenerates) use it, so that the Max
// retries per Shot is one limit over the Takes of both.
func CandidateRetries(takes int) int { return max(0, takes-1) }

// TriedActions are the strategies the Shot's repair history already carried out: those that made a
// new Take, and Edit-Level Repair (which makes none). A repair that failed before it made a Take
// does not count as tried.
func TriedActions(history []RepairRecord) []RepairAction {
	var out []RepairAction
	for _, r := range history {
		if (r.ResultingTakeID != "" || r.Action == RepairEditLevel) && !slices.Contains(out, r.Action) {
			out = append(out, r.Action)
		}
	}
	return out
}

// Reason is why the Retry Policy stopped the loop, for the Escalation card.
func (v RetryVerdict) Reason() string {
	switch v {
	case RetryMaxRetries:
		return "この Shot の再試行回数（Max retries per Shot）の上限に達しました"
	case RetryShotBudget:
		return "次の再生成が Shot の予算上限（Max cost per Shot）を超えます"
	case RetryProjectBudget:
		return "次の再生成が Project の予算上限を超えます"
	}
	return ""
}

// RepairShadow is one shadow run of Jev (spec/architecture/model-adapters.md §4.5): Jev was asked
// which strategy to use while the Repair Agent chose DecidedAction. It never influences the repair.
type RepairShadow struct {
	ID             string
	RepairActionID string
	// Model is the pinned Jev version the request used (never "jev-latest").
	Model         string
	JevChoice     string
	Confidence    *float64 // nil when the call failed
	Probabilities map[string]float64
	DecidedAction RepairAction
	Agreed        bool
	LatencyMillis int64
	InputTokens   int
	OutputTokens  int
	CostUSD       float64
	// Error is set when the call failed; the other result fields are then empty.
	Error     string
	CreatedAt time.Time
}

// ConfidenceBuckets is the number of equal-width confidence buckets of a ShadowReport.
const ConfidenceBuckets = 10

// ShadowReport summarises the shadow runs of Jev: the figures the decision to switch on its
// selection stage is made from (agreement with the Repair Agent, confidence distribution, latency, cost).
type ShadowReport struct {
	Runs   int `json:"runs"`
	Errors int `json:"errors"`
	Agreed int `json:"agreed"`
	// AgreementRate is Agreed / (Runs - Errors); 0 when nothing succeeded.
	AgreementRate  float64 `json:"agreement_rate"`
	MeanConfidence float64 `json:"mean_confidence"`
	// MeanLatencyMillis is over the successful runs, like the rate and the confidence (a failed run
	// stores no latency).
	MeanLatencyMillis float64 `json:"mean_latency_ms"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	// Confidence[i] counts the successful runs with confidence in [i/10, (i+1)/10) (1.0 is in the
	// last bucket); ConfidenceAgreed[i] counts those of them that agreed with the Repair Agent.
	Confidence       [ConfidenceBuckets]int `json:"confidence_histogram"`
	ConfidenceAgreed [ConfidenceBuckets]int `json:"confidence_agreed"`
}
