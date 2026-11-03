// Package llmlocal is the card "ローカル LLM サーバー" of the category "テキスト生成（LLM）" of the settings screen (#1008): the base URL of the
// local OpenAI-compatible LLM server (Ollama, LM Studio, llama.cpp server), its optional API key, the connection test and the switch that unloads the Ollama
// models before a local image or video generation. It shows whether a key is set and never the key.
package llmlocal

import "github.com/ousiassllc/mam/backend/internal/apperr"

// Data is what the card is drawn from.
type Data struct {
	BaseURL        string
	DefaultBaseURL string
	KeyConfigured  bool
	// KeyHint is the last characters of the stored key ("" without a key).
	KeyHint       string
	KeyUnreadable bool
	Unload        bool
	Pinned        bool
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

// The ids the card and its script share.
const (
	// CardID is the id of the card: what a save replaces.
	CardID = "llm-local-card"
	// ResultID is where the answer of the connection test is put.
	ResultID = "llm-local-result"
)
