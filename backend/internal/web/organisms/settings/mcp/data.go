// Package mcp is the "MCP サーバー" category of the global settings screen (spec/components/settings-screen-categories.md
// §4): the address the server listens on and the modal that changes it, the bearer tokens as a table and the modal that
// issues one, which shows the value once (spec/architecture/mcp.md §9). The tokens' values are never part of a list: only
// the start of each, which tells them apart.
package mcp

import "time"

// TokenRow is one issued token. It never carries the value, only the start of it that tells tokens apart.
type TokenRow struct {
	ID         string
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time // nil: never used
	Revoked    bool
}

// NewToken is a token that was just issued. Its value is shown here, once, and never again.
type NewToken struct {
	Name  string
	Value string
}

// Data is the category. Addr is where the server listens now (empty: not listening), Saved the bind address in mcp.json,
// Problem why the two differ or why MCP is off, Candidates the addresses of this machine the bind address can be set to.
// Error is why the tokens could not be read.
type Data struct {
	Addr       string
	Saved      string
	Problem    string
	Candidates []string
	Tokens     []TokenRow
	Error      string
	MaxNameLen int
}

// BindForm is the modal that changes the bind address: the address it holds (the saved one, or what was typed and
// refused) and, after a refusal, why.
type BindForm struct {
	Address    string
	Candidates []string
	Error      string
}

// IssueForm is the modal that issues a token: the name it holds (what was typed and refused) and, after a refusal, why.
type IssueForm struct {
	Name       string
	MaxNameLen int
	Error      string
}

// The ids that the actions replace or add to, and the focus keys of the buttons that open the modals.
const (
	BindRowID   = "mcp-bind-row"
	TableBodyID = "mcp-token-list"
	// NoRowsID is the row that says there are none, which the first token takes away.
	NoRowsID = "mcp-tokens-empty"

	BindFocusKey  = "edit-bind"
	IssueFocusKey = "issue-token"
)

// TokenRowID is the id of the row of a token: what a revoke replaces.
func TokenRowID(id string) string { return "mcp-token-" + id }
