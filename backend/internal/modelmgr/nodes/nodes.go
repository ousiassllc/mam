// Package nodes installs, checks and removes the custom nodes of the allowlist (modelmgr/nodecat) in the bundled ComfyUI
// (spec/architecture/model-manager/custom-nodes.md). A node is the pinned GitHub archive of one commit: it is downloaded, checked against its
// SHA-256, extracted below <data>/custom_nodes, and recorded with a digest of the extracted tree. The engine is started with
// --whitelist-custom-nodes naming only the folders Enabled returns, so a folder that is not in the allowlist, that was put there by hand,
// or that no longer matches what was installed is never loaded. mam runs no pip and no script of the node: extraction only.
package nodes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// Errors of the manager.
var (
	// ErrUnknown: the node is not in the allowlist.
	ErrUnknown = errors.New("nodes: not in the allowlist")
	// ErrBlocked: the node is in the allowlist but mam does not install it (nodecat.Node.InstallBlocked).
	ErrBlocked = errors.New("nodes: the node cannot be installed")
	// ErrNotInstalled: there is nothing to remove.
	ErrNotInstalled = errors.New("nodes: not installed")
	// ErrNotAPackage: the archive holds no __init__.py at its top: ComfyUI would load nothing from it.
	ErrNotAPackage = errors.New("nodes: the archive is not a ComfyUI custom node package (no __init__.py)")
)

// Phase is what Install is doing.
type Phase string

// The phases of Install, in order.
const (
	PhaseDownload Phase = "download"
	PhaseExtract  Phase = "extract"
)

// Progress receives the phase and the bytes done and in all of that phase. It must not block.
type Progress func(p Phase, done, total int64)

// State is where a node stands.
type State string

// The states of a node. Only StateInstalled is loaded by the engine.
const (
	// StateNotInstalled: no record. A folder of the name that is there anyway is not loaded either (Detail says so).
	StateNotInstalled State = "not_installed"
	// StateInstalled: recorded, the record is the pinned commit and archive, and the folder is what was extracted.
	StateInstalled State = "installed"
	// StateOutdated: recorded, but this mam pins another commit or archive: not loaded until it is installed again.
	StateOutdated State = "outdated"
	// StateModified: recorded, but the folder is missing or differs from what was extracted: not loaded until it is installed again.
	StateModified State = "modified"
	// StateBlocked: the allowlist lists the node without installing it (a Python package it needs is not in the bundled environment).
	StateBlocked State = "blocked"
)

// Status is a node of the allowlist with its state.
type Status struct {
	Node  nodecat.Node
	State State
	// Detail says what is wrong for a state that is not plain (Modified, Outdated, a stray folder of a NotInstalled node).
	Detail    string
	Installed *Installed
}

// Manager installs the nodes. The zero value of the optional fields is the production setting. A nil *Manager has no node installed.
type Manager struct {
	Layout store.Layout
	// Catalog is the allowlist (nil: nodecat.Default()).
	Catalog *nodecat.Catalog
	// Extract unpacks the archive held in r (size bytes; default extract.ZipFrom).
	Extract func(ctx context.Context, r io.ReaderAt, size int64, dest, strip string, limit int64, progress extract.Progress) error
	// Fetch are the options of the download (client, URL check); Progress is set per download.
	Fetch     fetch.Options
	FreeBytes func(path string) (uint64, error)
	Now       func() time.Time

	work sync.Mutex // an install or a remove at a time
	rec  *recordFile
}

// New returns the Manager of the layout.
func New(layout store.Layout) *Manager {
	return &Manager{Layout: layout, rec: &recordFile{path: layout.NodesPath()}}
}

// Allowlist is the allowlist the manager works with (the embedded one unless Catalog is set; a nil Manager has the embedded one).
func (m *Manager) Allowlist() *nodecat.Catalog {
	if m != nil && m.Catalog != nil {
		return m.Catalog
	}
	return nodecat.Default()
}

func (m *Manager) catalog() *nodecat.Catalog { return m.Allowlist() }

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Folder is where the node lies: <data>/custom_nodes/<folder>.
func (m *Manager) Folder(n nodecat.Node) string {
	return filepath.Join(m.Layout.CustomNodesDir(), n.Folder)
}

// Status is the state of the node with the ID.
func (m *Manager) Status(id string) (Status, error) {
	n, ok := m.catalog().Node(id)
	if !ok {
		return Status{}, ErrUnknown
	}
	return m.status(n, m.record()), nil
}

// List is every node of the allowlist with its state, in the order of the allowlist.
func (m *Manager) List() []Status {
	rec := m.record()
	out := make([]Status, 0, len(m.catalog().Nodes))
	for _, n := range m.catalog().Nodes {
		out = append(out, m.status(n, rec))
	}
	return out
}

func (m *Manager) record() Record {
	if m == nil {
		return Record{}
	}
	return m.rec.get()
}

// status judges a node: the record has to be the pinned commit and archive, and the folder the tree that was extracted.
func (m *Manager) status(n nodecat.Node, rec Record) Status {
	st := Status{Node: n}
	inst, recorded := rec.Nodes[n.ID]
	switch {
	case !n.Installable():
		st.State = StateBlocked
	case !recorded:
		st.State = StateNotInstalled
		if _, err := os.Lstat(m.Folder(n)); err == nil {
			st.Detail = "記録のないフォルダ " + n.Folder + " があります。mam が導入したものではないため、読み込みません"
		}
	case inst.Commit != n.Commit || inst.ArchiveSHA256 != n.ArchiveSHA256:
		st.State, st.Installed = StateOutdated, &inst
		st.Detail = "導入したコミット " + short(inst.Commit) + " は、この mam が許可するコミット " + short(n.Commit) + " と違います。導入し直すまで読み込みません"
	default:
		st.Installed = &inst
		if sum, err := TreeDigest(m.Folder(n)); err != nil || sum != inst.TreeSHA256 {
			st.State = StateModified
			st.Detail = "フォルダ " + n.Folder + " の中身が、導入したときと違う（または無い）ため、読み込みません。導入し直してください"
		} else {
			st.State = StateInstalled
		}
	}
	return st
}

func short(commit string) string { return commit[:min(8, len(commit))] }

// Enabled is the folders the engine may load: the nodes that are installed, pinned and unchanged, in the order of the allowlist. It is what
// --whitelist-custom-nodes is given. Everything else in custom_nodes is skipped by ComfyUI.
func (m *Manager) Enabled() []string {
	if m == nil {
		return nil
	}
	var out []string
	for _, st := range m.List() {
		if st.State == StateInstalled {
			out = append(out, st.Node.Folder)
		}
	}
	return out
}

// Missing is the IDs of nodes that the engine does not load now (not installed, outdated, modified, blocked or unknown), in the order given.
// Only the nodes asked for are judged (a judgment hashes the folder), so no ID costs nothing.
func (m *Manager) Missing(ids []string) []string {
	var out []string
	var rec Record
	if m != nil {
		rec = m.record()
	}
	for _, id := range ids {
		if slices.Contains(out, id) {
			continue
		}
		if m != nil {
			if n, ok := m.catalog().Node(id); ok && m.status(n, rec).State == StateInstalled {
				continue
			}
		}
		out = append(out, id)
	}
	return out
}

// CheckSpace returns a *fetch.NoSpaceError when the disk cannot hold the archive and its extraction.
func (m *Manager) CheckSpace(n nodecat.Node) error {
	free := m.FreeBytes
	if free == nil {
		free = fsutil.FreeBytes
	}
	have, err := free(m.Layout.Root)
	if err != nil {
		return fmt.Errorf("free space: %w", err)
	}
	need := uint64(n.ArchiveSize + n.ExtractedSize) //nolint:gosec // sizes of the catalog, never negative
	if need > have {
		return &fetch.NoSpaceError{Need: need, Free: have, Path: m.Layout.Root}
	}
	return nil
}
