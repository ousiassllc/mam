// Package llm is the "テキスト生成（LLM）" category of the settings screen (spec/components/settings-screen-categories.md
// §2): a table with a row for each Agent (what it is for, its primary and fallback LLM, whether it differs from the
// default), and the modal that edits one Agent. A save is one Agent's; the whole table is saved by PUT /settings/llm
// (kept for the MCP tools and the tests) and reset by DELETE /settings/llm.
package llm

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
	// Restart: the row was saved just now. What runs is not what is saved until the app restarts, which the row says. It is
	// gone when the page is read again (the service does not know what runs).
	Restart bool
}

// Customized is whether the row differs from the default.
func (r Row) Customized() bool {
	return r.Primary != r.DefaultPrimary || r.Fallback != r.DefaultFallback
}

// Data is the category: Rows are the Agents that run; Unused are the Agents whose route no code reads yet, listed apart.
// Providers are the choices of a provider. Pinned means the operator fixed the file by a start option: the assignment is
// shown and cannot be edited. Problem is why the saved file could not be read; Notice is a success of the whole table
// (a reset), Error is why a save of the whole table was refused. MaxModelLength is the longest model name the server
// accepts.
type Data struct {
	Rows           []Row
	Unused         []Row
	Providers      []string
	Pinned         bool
	Problem        string
	Notice         string
	Error          string
	MaxModelLength int
}

// Form is what the modal of an Agent is drawn from: its row and, after a refusal, why and what was typed (Row holds it).
type Form struct {
	Row            Row
	Providers      []string
	MaxModelLength int
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
