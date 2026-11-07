// Package ollamacard is the section "Ollama のモデル" of the page "ローカル LLM" (#1105; spec/components/model-manager-screen.md, "Ollama の導入とモデルの取得"): the
// state of the Ollama that mam manages, a card for each of the three models of the catalog with the buttons to fetch, cancel and delete, the models the server holds that
// are not in the list, and the detail modal with the layers, the SHA-256 and the check of the manifest. It is a package of its own because the modelmgr package is at its size limit.
package ollamacard

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// SectionID is the section (the anchor of the messages and of the MCP tools: #mm-ollama); StateID the line that says where the server stands.
const (
	SectionID = "mm-ollama"
	StateID   = "mm-ollama-state"
)

// RuntimeAnchor is the card of the Ollama runtime on the same page, where it is installed and started.
const RuntimeAnchor = "#mm-runtime-ollama"

// The states of a model (ollamapull.State) and of the server (ollamapull.Server) that the templates read.
const (
	StateMissing     = "missing"
	StatePulling     = "pulling"
	StateInstalled   = "installed"
	StateChanged     = "changed"
	StateUnavailable = "unavailable"

	ServerUnsupported  = "unsupported"
	ServerNotInstalled = "not_installed"
	ServerInstalling   = "installing"
	ServerStopped      = "stopped"
	ServerStarting     = "starting"
	ServerRunning      = "running"
	ServerFailed       = "failed"
)

// ModelID is the id of the card of a model: the name with the characters an id should not have turned into "-" ("gemma4:12b" → "mm-ollama-gemma4-12b").
func ModelID(name string) string {
	return "mm-ollama-" + strings.NewReplacer(":", "-", ".", "-").Replace(name)
}

// Model is the card of a model of the catalog.
type Model struct {
	view.Message
	// Name is "<name>:<tag>" (what is fetched); DisplayName what the card calls it.
	Name        string
	DisplayName string
	Description string
	// Size is what a pull transfers, in words.
	Size  string
	State string
	// Commercial is the answer for commercial use.
	Commercial commercial.View
	// Install is the progress of a running pull (nil when none).
	Install *view.Progress
	// NoSpace says the disk of the models has not the room a pull needs (the model and 1 GiB): the card says so, and a pull is refused with the sizes.
	NoSpace bool
	// Blocked says why a pull cannot be asked now ("" when it can): the server is not installed, cannot run here or is being installed.
	Blocked string
}

// Other is a model the server holds that is not in the list: its name and size.
type Other struct {
	Name string
	Size string
}

// Section is the section: the server, the models of the list and the others.
type Section struct {
	view.Message
	// Server is where the Ollama of mam stands; Reason says why it cannot be used ("" when it can).
	Server string
	Reason string
	Items  []Model
	Others []Other
	// Jobs are the pulls the section reports on: while one runs, the section polls itself.
	Jobs []string
	// OOB draws the section out of band into the page that has it (the answer of an action of the runtime card).
	OOB bool
}

// Polling says the section asks for itself again: a pull is running.
func (s Section) Polling() bool { return len(s.Jobs) > 0 }

func (s Section) url() string {
	u := paths.ModelManagerPart("ollama")
	if len(s.Jobs) > 0 {
		u += "?jobs=" + strings.Join(s.Jobs, ",")
	}
	return u
}

// StateText is the line under the heading: where the server stands, in words.
func (s Section) StateText() string {
	switch s.Server {
	case ServerNotInstalled:
		return "Ollama は未導入です。"
	case ServerInstalling:
		return "Ollama を導入しています。"
	case ServerUnsupported:
		return "Ollama は使えません: " + s.Reason
	case ServerStopped:
		return "Ollama は停止中です。取得のボタンを押すと起動します。"
	case ServerStarting:
		return "Ollama を起動しています。"
	case ServerFailed:
		return "Ollama の起動に失敗しました: " + s.Reason
	case ServerRunning:
		return "Ollama は起動中です。"
	}
	return ""
}

// RuntimeHint is what follows the link to the card of the runtime above in the line ("" when the line has no link): the server is not installed, is being installed or failed to start.
func (s Section) RuntimeHint() string {
	switch s.Server {
	case ServerNotInstalled:
		return "から導入してください。"
	case ServerInstalling:
		return "で、導入の進み具合を見られます。"
	case ServerFailed:
		return "から、起動し直してください。"
	}
	return ""
}

// BlockedReason is why a model cannot be fetched while the server stands as server ("" when it can).
func BlockedReason(server string) string {
	switch server {
	case ServerNotInstalled:
		return "先に Ollama を導入してください"
	case ServerInstalling:
		return "Ollama の導入が終わってから取得してください"
	case ServerUnsupported:
		return "この PC では Ollama を使えません"
	}
	return ""
}

// Layer is a blob of the manifest of a model.
type Layer struct {
	Kind   string
	Size   string
	Digest string
}

// Detail is the modal of a model: the layers with their sizes, the SHA-256 of the weights, what the check of the manifest says, the license.
type Detail struct {
	Name        string
	DisplayName string
	Description string
	State       string
	Commercial  commercial.View
	// Size is the total a pull transfers; Layers the blobs; Digest the SHA-256 of the weights (the layer of kind "model").
	Size   string
	Layers []Layer
	Digest string
	// Check is what the comparison of the manifest on disk with the record says ("" while the model is not fetched): the sentence.
	Check string
	// Changed says the manifest differs from the record.
	Changed        bool
	CheckedAt      string
	LicenseName    string
	LicenseURL     string
	LicenseSummary string
	SourceURL      string
}
