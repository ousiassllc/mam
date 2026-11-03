// Package llm is the "テキスト生成（LLM）" category of the settings screen (spec/components/settings-screen-categories.md
// §2): a table with a row for each Agent (what it is for, its primary and fallback LLM, whether it differs from the
// default), and the modal that edits one Agent. A save is one Agent's; the whole table is saved by PUT /settings/llm
// (kept for the MCP tools and the tests) and reset by DELETE /settings/llm.
package llm

import "github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"

// Target is one provider and one model name.
type Target struct {
	Provider string
	Model    string
}

// Summary is the target as one line: "anthropic / claude-…".
func (t Target) Summary() string { return t.Provider + " / " + t.Model }

// Row is the assignment of one Agent: what it is for, the primary target, the fallback used when it fails, and the
// defaults the program ships (shown beside the fields of the modal, so that a change can be undone by hand).
type Row struct {
	Agent   string
	Label   string
	Purpose string // what the Agent is for, in one line
	Primary Target
	// Fallback is the target the Agent switches to when the primary fails.
	Fallback        Target
	DefaultPrimary  Target
	DefaultFallback Target
}

// Customized is whether the row differs from the default.
func (r Row) Customized() bool {
	return r.Primary != r.DefaultPrimary || r.Fallback != r.DefaultFallback
}

// Warning is a saved route that names a provider that cannot be used now: the sentence, and the screen that fixes it (Route; "" when none helps).
type Warning struct{ Message, Route string }

// Data is the category: Rows are the Agents that run; Unused are the Agents whose route no code reads yet, listed apart.
// Pinned means the operator fixed the file by a start option: the assignment is
// shown and cannot be edited. Problem is why the saved file could not be read; Notice is a success of the whole table
// (a reset), Error is why a save of the whole table was refused. MaxModelLength is the longest model name the server
// accepts.
type Data struct {
	Rows    []Row
	Unused  []Row
	Pinned  bool
	Problem string
	// Warnings say which saved routes name a provider that cannot be used now (no API key, not signed in), each with where to fix it (#1013).
	Warnings       []Warning
	Notice         string
	Error          string
	MaxModelLength int
}

// ModelField is the model field of one target: the model picker (#1011) that chooses the provider and the model together, and what the modal says besides.
type ModelField struct {
	// Role is "primary" / "fallback".
	Role     string
	Provider string
	// Selected is the model the field shows as chosen; Unlisted is whether the provider does not list it (a saved name that stays, with a warning, and can be saved).
	Selected string
	Unlisted bool
	// Field is the picker's field: the hidden inputs "<role>.provider" and "<role>.model", and the card of what is chosen.
	Field modelpick.FieldData
	// Unusable says why the provider cannot be used now (no API key, not signed in) and Route where it is fixed; "" when it can. A saved model that cannot be used is
	// still kept: the key may be added later.
	Unusable, Route string
	// Default is the model the program ships for this target.
	Default Target
}

// Form is what the modal of an Agent is drawn from: its row and, after a refusal, why and what was typed (Row holds it).
type Form struct {
	Row            Row
	MaxModelLength int
	// Primary and Fallback are the model fields of the two targets.
	Primary, Fallback ModelField
	// Error is why the last save was refused; Blame is the field it is about ("primary.model": the target and the field),
	// which carries aria-invalid and the focus. Empty when no field is to blame.
	Error, Blame string
}

// The ids the table and the script share.
const (
	// TableID is the list of the Agents that run; UnusedTableID the list of those that do not.
	TableID       = "llm-table"
	UnusedTableID = "llm-unused-table"
	// PanelID is what the whole table's answers replace.
	PanelID = "llm-assignment-panel"
)

// RowID is the id of the row of an Agent: what a save replaces out of band.
func RowID(agent string) string { return "llm-row-" + agent }

// FocusKey names the button that opens the modal of an Agent (it stays the same whatever the row says).
func FocusKey(agent string) string { return "edit-" + agent }

func fieldID(role, field string) string { return "llm-" + role + "-" + field }

// FieldID is the id of the model field of a role: its picker button is FieldID(role)+"-button".
func FieldID(role string) string { return fieldID(role, "model") }

// Warnings are the saved routes that name a provider that cannot be used now (#1013): each one a sentence; the assignment screen shows them above the table.
