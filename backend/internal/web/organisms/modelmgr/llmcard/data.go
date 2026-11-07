// Package llmcard is the page "ローカル LLM" of the model manager (spec/components/model-manager-screen.md, "ローカル LLM の統合（#1104）"): the section "使うサーバー" (which server the calls
// of the provider "local" go to), and the section "別のサーバーにつなぐ" (the address, the optional API key, the connection test and the switch that unloads the Ollama models before
// a local image or video generation). The section of the servers that mam runs itself is runtimecard's. It shows whether a key is set and never the key. It is a package of its own because
// the modelmgr package is at its size limit.
package llmcard

import "github.com/ousiassllc/mam/backend/internal/apperr"

// The ids of the elements the actions swap.
const (
	UseID        = "mm-llm-use"
	UseStateID   = "mm-llm-use-state"
	ConnectionID = "mm-llm-connection"
	// ResultID is where the answer of the connection test is put.
	ResultID = "mm-llm-connection-result"
)

// FieldUse is the name of the radio group of the section "使うサーバー" (the values are llmlocal.Use).
const FieldUse = "use"

// Choice is one server of the radio group.
type Choice struct {
	// Use is the value of the radio: llama-cpp, ollama or custom.
	Use  string
	Name string
	// About is one line that says what it is.
	About string
	// State is where a server of mam stands in words ("導入済み・停止中"); empty for another server.
	State string
}

// Use is the section "使うサーバー".
type Use struct {
	// Choices are the servers the app can send the calls to, in the order llama-cpp, ollama, custom.
	Choices []Choice
	// Selected is the value of the one in force.
	Selected string
	// NoManaged is why the servers of mam are not offered (the app has none); empty when one is.
	NoManaged string
	Pinned    bool
	// Problem is why the settings file could not be read.
	Problem string
	State   State
	// Notice is what the last save did; Error is why it was refused.
	Notice, Error string
}

// State is the line of the state of the server in use: the sentence, why it cannot be used, and whether it asks for itself again.
type State struct {
	// Line is "今は <名前> に呼び出します"; Problem is why that server cannot be used now (empty when it can).
	Line, Problem string
	// Kind is the data-state: ready, attention, installing or empty.
	Kind string
	// Polling is true while the server installs or starts: the line reads itself again until that is over.
	Polling bool
}

// Connection is the section "別のサーバーにつなぐ".
type Connection struct {
	BaseURL        string
	DefaultBaseURL string
	KeyConfigured  bool
	// KeyHint is the last characters of the stored key ("" without a key).
	KeyHint       string
	KeyUnreadable bool
	Unload        bool
	// Note is true while the calls go to a server of mam: the address below is for "別のサーバー" and not in force now.
	Note   bool
	Pinned bool
	// Problem is why the settings file could not be read (a save is refused until it is repaired).
	Problem string
	// Notice is what the last save did; Error is why it was refused, and Blame the field it is about ("base_url" / "api_key").
	Notice, Error, Blame string
}

// TestResult is the outcome of a connection test, drawn under the form.
type TestResult struct {
	OK      bool
	Message string
	// Failure is the common error of a failed test (apperr.View): the cause and the remedy.
	Failure *apperr.View
	Models  []string
	Count   int
}
