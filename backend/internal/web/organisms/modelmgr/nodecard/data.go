// Package nodecard is the section "追加ノード" of the model manager screen: a card for every custom node of the allowlist (the nodes the
// bundled ComfyUI may load) with its state, license and commercial use, install and removal, and the detail with the pinned commit, the
// SHA-256, the dependencies and the review (spec/components/model-manager-screen.md). It is a package of its own because the modelmgr package
// is at its size limit.
package nodecard

import (
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// IDs of the elements the actions swap.
const (
	// SectionID is the section "追加ノード" (the anchor the messages of the screen and the MCP tools point to: #mm-nodes).
	SectionID = "mm-nodes"
	// DialogID is where the consent dialog of an install opens.
	DialogID = "mm-nodes-dialog"
)

// NodeID is the id of the card of a node (the element the actions of the card swap).
func NodeID(id string) string { return "mm-node-" + id }

// Node is the card of a custom node of the allowlist.
type Node struct {
	view.Message
	ID          string
	Name        string
	Description string
	Repo        string
	// Commit is the pinned commit (40 hex digits); ArchiveSHA the SHA-256 of its archive.
	Commit     string
	ArchiveSHA string
	Size       string
	Extracted  string
	Need       string

	LicenseID      string
	LicenseVersion string
	LicenseName    string
	LicenseURL     string
	LicenseSummary string
	// LicenseCommercial is the answer in words ("可", "不明（確認できていません）"); Commercial the badge and icon.
	LicenseCommercial string
	LicenseNotice     string
	Commercial        commercial.View
	CheckedAt         string

	PythonRequires string
	PythonOptional string
	Limits         string
	Classes        string
	ReviewedAt     string
	Review         string

	// State is not_installed, installed, outdated, modified or blocked; Detail says what is wrong for the ones that are not plain.
	State  string
	Detail string
	// Blocked is why mam does not install the node (empty: it does).
	Blocked string
	// InstalledCommit and InstalledAt are what was installed, and when.
	InstalledCommit string
	InstalledAt     string
	// Unavailable is why no node can be installed now ("" when one can).
	Unavailable string
	// Accepted: the license and the security notice were accepted in their current versions.
	Accepted       bool
	NoticeVersion  string
	SecurityNotice string

	Install *view.Progress
	// Job is the install job the card reports on (running, or one that just ended).
	Job string
	// Models are the models of the catalog that need the node.
	Models []Model
}

// Model is a model that needs the node.
type Model struct {
	Name      string
	Installed bool
}

// Polling reports whether the card has an install in progress.
func (n Node) Polling() bool { return n.Install != nil }

// Section is the section "追加ノード".
type Section struct {
	view.Message
	Items []Node
}

func nodeURL(n Node) string {
	base := paths.ModelManagerPart("nodes", n.ID)
	if n.Job == "" {
		return base
	}
	return base + "?job=" + url.QueryEscape(n.Job)
}

func detailURL(n Node) string { return paths.ModelManagerPart("nodes", n.ID, "detail") }
