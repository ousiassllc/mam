package commandinterpreter

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/budget"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Operations that change a setting of the Project (spec/spec.md §24). They are described in the registry below: a new one is one OpType, one Spec and
// the service's handler (service/director_ops.go) — the prompt, the guard on the model's word, the cleaning of its answer and the question of the chat
// card all follow from the registry, not from a switch in each place.
const (
	// OpSetBudget changes the budget ceilings of the Project ("予算を $30 に変えて", "1 ショット $3 まで"). The chat asks for confirmation first: the
	// ceiling is money (#1139).
	OpSetBudget OpType = "set_budget"
	// OpSetQualityPolicy changes the Quality Policy (FAST / BALANCED / MAX QUALITY), which fixes the models, the resolution and the number of Takes. It is
	// not asked of the Interpreter: the options the chat offers for staying within the budget carry it as the action of their button.
	OpSetQualityPolicy OpType = "set_quality_policy"
)

// Spec is what the chat and the service need to know about an operation beyond what the interpreter's own switch handles.
type Spec struct {
	// Example is the line of the system prompt's JSON example, and Doc the bullet of its "Operations:" list. An operation the model is not asked for has none.
	Example, Doc string
	// Clean validates the operation the model returned (and fills in what follows from it); false drops it.
	Clean func(op Operation) (Operation, bool)
	// Asked reports whether the message really asks for the operation: the model's word alone is not enough for an operation that changes settings or
	// makes a note (nil: it is enough).
	Asked func(message string) bool
	// AnyMode is true for an operation carried out in AUTO MODE too (every other operation is a direction of DIRECTOR MODE).
	AnyMode bool
	// Confirm is true for an operation that is not carried out at once: the chat posts a confirmation card and carries it out when the user accepts it.
	Confirm bool
	// Global is true for an operation on the settings of the whole app (an API key, a model assignment, an install: #1140), not of the Project: the chat reads it in
	// any state of the Project past the Brief (while the Brief is drafted only a Setup one), and the card says that it affects every Project.
	Global bool
	// Setup is true for a Global operation that the chat reads while the Brief is still being drafted (DRAFT): what a new app needs before anything else (the API key of the LLM,
	// the route of an LLM). Every other message of a DRAFT Project is an answer to the Brief, however much it sounds like a setting ("動画は縦型にして").
	Setup bool
	// Immediate is true for an operation that changes nothing and has no card: the chat carries it out at once and tells what it found (a download link).
	Immediate bool
	// Detect reads the operations a message plainly asks for without the model, so that the chat can still do them while no LLM is set up (the API key of the LLM
	// itself is the first thing to set). nil: only the model reads the operation.
	Detect func(message string) []Operation
}

var specs = map[OpType]Spec{
	OpCreateNote: {AnyMode: true, Asked: domain.AsksToRemember},
	OpSetBudget: {
		Example: `{"type": "set_budget", "project_budget_usd": 30, "shot_budget_usd": 3}`,
		Doc: `- "set_budget": the director wants the budget ceilings changed ("予算を $30 に変えて", "予算は 50 ドルまで", "1 ショットあたり $3 まで", "上限を外して"). ` +
			`"project_budget_usd" is the ceiling of the whole project and "shot_budget_usd" the ceiling of one shot, in US dollars. Name only the ceiling the director names; ` +
			`the other one stays as it is. 0 removes a ceiling. Amounts the director states in another currency cannot be set: use "unsupported" with a reason that asks for dollars.` + "\n",
		Clean: cleanSetBudget, Asked: budget.Mentions, AnyMode: true, Confirm: true,
	},
	OpSetQualityPolicy: {Clean: cleanSetQualityPolicy},
}

// SpecOf is the registry entry of an operation; false for the operations of the interpreter's own switch (set_lock, regenerate, ...).
func SpecOf(t OpType) (Spec, bool) { s, ok := specs[t]; return s, ok }

// Confirms reports whether the operation waits for the user's confirmation before it is carried out.
func (o Operation) Confirms() bool { return specs[o.Type].Confirm }

// Immediate reports whether the operation is carried out at once without a card (Spec.Immediate).
func (o Operation) Immediate() bool { return specs[o.Type].Immediate }

// AnyMode reports whether the operation is carried out outside DIRECTOR MODE.
func (o Operation) AnyMode() bool { return specs[o.Type].AnyMode }

// AsksAnyMode reports whether the message is a direction for an operation of any mode other than the note (a note has its own check,
// domain.AsksToRemember): the chat reads such a message with the interpreter whatever the mode.
func AsksAnyMode(message string) bool {
	for t, s := range specs {
		if s.AnyMode && s.Asked != nil && t != OpCreateNote && s.Asked(message) {
			return true
		}
	}
	return false
}

// AsksGlobal reports whether the message is a direction for an operation on the settings of the whole app (Spec.Global), or one the chat refuses on purpose (Refused): the chat
// reads it in every state of the Project past the Brief.
func AsksGlobal(message string) bool { return asks(message, func(s Spec) bool { return s.Global }) }

// AsksSetup is AsksGlobal for the Brief that is still drafted: only the operations the new app needs first (Spec.Setup) and the refusals.
func AsksSetup(message string) bool {
	return asks(message, func(s Spec) bool { return s.Global && s.Setup })
}

func asks(message string, wanted func(Spec) bool) bool {
	if _, refused := Refused(message); refused {
		return true
	}
	for _, s := range specs {
		if wanted(s) && s.Asked != nil && s.Asked(message) {
			return true
		}
	}
	return false
}

// Unasked drops from ops the operations the message does not really ask for (Spec.Asked).
func Unasked(ops []Operation, message string) []Operation {
	return slices.DeleteFunc(slices.Clone(ops), func(op Operation) bool {
		asked := specs[op.Type].Asked
		return asked != nil && !asked(message)
	})
}

// cleanRegistered is clean for the operations of the registry the model is asked for: it never invents one that has no prompt entry.
func cleanRegistered(op Operation) (Operation, bool) {
	if s, ok := specs[op.Type]; ok && s.Example != "" {
		return Normalize(op)
	}
	return Operation{}, false
}

// Normalize validates an operation of the registry, as the model returned it or as a chat card stored it as the action of a button; false when it is none
// or invalid.
func Normalize(op Operation) (Operation, bool) {
	if s, ok := specs[op.Type]; ok && s.Clean != nil {
		return s.Clean(op)
	}
	return Operation{}, false
}

// registryTypes lists the registered operations with a prompt entry, in a fixed order.
func registryTypes() []OpType {
	var types []OpType
	for t, s := range specs {
		if s.Example != "" {
			types = append(types, t)
		}
	}
	slices.Sort(types)
	return types
}

// registryExamples is the examples of the registered operations, each on a line of its own after the built-in ones.
func registryExamples() string {
	var b strings.Builder
	for _, t := range registryTypes() {
		b.WriteString(",\n    " + specs[t].Example)
	}
	return b.String()
}

// registryDocs is the bullets of the registered operations.
func registryDocs() string {
	var b strings.Builder
	for _, t := range registryTypes() {
		b.WriteString(specs[t].Doc)
	}
	return b.String()
}

// BudgetCeilings are the budget ceilings of the Project in USD; nil is "none".
type BudgetCeilings struct{ Project, Shot *float64 }

func (c BudgetCeilings) String() string {
	return fmt.Sprintf("project: %s / shot: %s", usdText(c.Project), usdText(c.Shot))
}

func usdText(v *float64) string {
	if v == nil {
		return "none"
	}
	return fmt.Sprintf("$%.2f", *v)
}

// cleanSetBudget keeps the ceilings the model named: a number that is not finite or is negative is not an amount. 0 stays, it removes the ceiling.
func cleanSetBudget(op Operation) (Operation, bool) {
	out := Operation{Type: OpSetBudget, ProjectBudgetUSD: usdAmount(op.ProjectBudgetUSD), ShotBudgetUSD: usdAmount(op.ShotBudgetUSD)}
	return out, out.ProjectBudgetUSD != nil || out.ShotBudgetUSD != nil
}

func usdAmount(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return nil
	}
	return v
}

func cleanSetQualityPolicy(op Operation) (Operation, bool) {
	if !op.Quality.Valid() {
		return Operation{}, false
	}
	return Operation{Type: OpSetQualityPolicy, Quality: op.Quality}, true
}
