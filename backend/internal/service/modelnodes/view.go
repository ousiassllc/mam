package modelnodes

import (
	"context"
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// NoticeVersion is the version of the security notice (SecurityNotice): changing the wording asks for a new acceptance.
const NoticeVersion = "2026-11-03"

// SecurityNotice is what the user accepts before a node is installed, besides its license. A custom node is a program that runs inside
// ComfyUI with the rights of the user: it can read and write the user's files and use the network. mam only installs nodes it reviewed and
// pinned (the commit and the SHA-256), and only those are loaded.
const SecurityNotice = "追加ノードは、ComfyUI の中でそのまま実行されるプログラム（Python のコード）です。mam を動かしているあなたのアカウントの権限で、ファイルの読み書きやネットワーク通信ができます。" +
	"mam は、許可リストに載せたノードだけを、確認したコミットのままの内容で導入し（SHA-256 を検証）、それ以外のフォルダは読み込みません。ただし、確認は mam の開発時に 1 度行ったもので、コードの安全を保証するものではありません。"

// ModelRef is a model of the catalog that needs a node.
type ModelRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

// View is a node of the allowlist for the list: the pins, the license and commercial use, the dependencies and the review, and where the
// node stands.
type View struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Repo        string `json:"repo"`
	Commit      string `json:"commit"`
	ArchiveSHA  string `json:"archive_sha256"`
	ArchiveSize int64  `json:"archive_size"`
	// ExtractedSize is the room the extracted files take.
	ExtractedSize int64 `json:"extracted_size"`
	// Folder is the name of the folder under custom_nodes.
	Folder string `json:"folder"`
	// License is the license of the node's code (with the commercial-use answer the screens show).
	License catalog.License `json:"license"`
	// PythonRequires are the packages the node imports; PythonOptional the ones only some of its nodes import (they fail without).
	PythonRequires []string `json:"python_requires"`
	PythonOptional []string `json:"python_optional,omitempty"`
	// InstallBlocked is why mam does not install the node (empty: it does).
	InstallBlocked string   `json:"install_blocked,omitempty"`
	NodeClasses    []string `json:"node_classes"`
	Limits         string   `json:"limits,omitempty"`
	ReviewedAt     string   `json:"reviewed_at"`
	Review         string   `json:"review"`
	// State is not_installed, installed, outdated, modified or blocked; Detail says what is wrong for the ones that are not plain.
	State  nodes.State `json:"state"`
	Detail string      `json:"detail,omitempty"`
	// InstalledCommit and InstalledAt are what was installed (when it was).
	InstalledCommit string     `json:"installed_commit,omitempty"`
	InstalledAt     *time.Time `json:"installed_at,omitempty"`
	// Accepted: the license and the security notice were accepted in their current versions. SecurityNotice is what is accepted besides the license.
	Accepted       bool       `json:"accepted"`
	AcceptedAt     *time.Time `json:"accepted_at,omitempty"`
	SecurityNotice string     `json:"security_notice"`
	// NoticeVersion is the version of SecurityNotice (the acceptance names it).
	NoticeVersion string `json:"notice_version"`
	// InstallJob is the install that is queued or running.
	InstallJob string `json:"install_job,omitempty"`
	// Models are the catalog models whose workflows need the node (they cannot be installed, or chosen, without it).
	Models []ModelRef `json:"models,omitempty"`
	// Unavailable is why no node can be installed now (the engine is not there, the folder cannot be written to); empty when one can.
	Unavailable string `json:"unavailable,omitempty"`
	// NeedBytes is the free room an install needs.
	NeedBytes int64 `json:"need_bytes"`
}

// List is every node of the allowlist.
func (s *Service) List(_ context.Context) []View {
	sts := s.Nodes.List()
	out := make([]View, 0, len(sts))
	for _, st := range sts {
		out = append(out, s.view(st))
	}
	return out
}

// Status is one node (modelmanager.ErrNotFound for an ID that is not in the allowlist).
func (s *Service) Status(_ context.Context, id string) (View, error) {
	st, err := s.Nodes.Status(id)
	if err != nil {
		return View{}, notFound(err)
	}
	return s.view(st), nil
}

func (s *Service) view(st nodes.Status) View {
	n := st.Node
	v := View{
		ID: n.ID, Name: n.Name, Description: n.Description, Repo: n.Repo, Commit: n.Commit, ArchiveSHA: n.ArchiveSHA256, ArchiveSize: n.ArchiveSize,
		ExtractedSize: n.ExtractedSize, Folder: n.Folder, License: n.License, PythonRequires: n.PythonRequires, PythonOptional: n.PythonOptional,
		InstallBlocked: n.InstallBlocked, NodeClasses: n.NodeClasses, Limits: n.Limits, ReviewedAt: n.ReviewedAt, Review: n.Review,
		State: st.State, Detail: st.Detail, SecurityNotice: SecurityNotice, InstallJob: s.activeJob(TargetPrefix + n.ID), NoticeVersion: NoticeVersion,
		NeedBytes: n.ArchiveSize + n.ExtractedSize, Unavailable: s.unavailable(),
	}
	if st.Installed != nil {
		at := st.Installed.InstalledAt
		v.InstalledCommit, v.InstalledAt = st.Installed.Commit, &at
	}
	if c, ok := s.Nodes.Consent(n.ID); ok && s.consentCurrent(n, c) {
		v.Accepted, v.AcceptedAt = true, &c.AcceptedAt
	}
	v.Models = s.modelsNeeding(n.ID)
	return v
}

// consentCurrent says the consent is for the license and the notice as they are now.
func (s *Service) consentCurrent(n nodecat.Node, c nodes.Consent) bool {
	return c.LicenseID == n.License.ID && c.LicenseVersion == n.License.Version && c.NoticeVersion == NoticeVersion
}

// unavailable is why no node can be installed now ("" when one can): the bundled engine exists on Windows only, and the nodes live next to it.
func (s *Service) unavailable() string {
	ev := s.Engine.Engine()
	switch {
	case !ev.Supported:
		return "同梱の ComfyUI は Windows 版だけで使えるため、この環境では追加ノードを導入できません。外部の ComfyUI を使う場合は、そちらにノードを入れてください"
	case !ev.Installed || ev.Broken:
		return "先に ComfyUI エンジンを導入してください"
	case !ev.Writable:
		return "mam.exe のあるフォルダに書き込めないため、追加ノードを導入できません"
	}
	return ""
}

// modelsNeeding are the models of the catalog whose workflows need the node, with whether each is installed.
func (s *Service) modelsNeeding(id string) []ModelRef {
	var out []ModelRef
	for _, m := range s.Engine.Catalog() {
		if slices.Contains(m.RequiresNodes, id) {
			out = append(out, ModelRef{ID: m.ID, Name: m.Name, Installed: m.State == modelmanager.ModelInstalled})
		}
	}
	return out
}
