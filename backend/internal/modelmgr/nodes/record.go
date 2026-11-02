package nodes

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// Installed is what was installed of a node: the pins it came from and the digest of the tree that was extracted.
type Installed struct {
	Commit        string    `json:"commit"`
	ArchiveSHA256 string    `json:"archive_sha256"`
	TreeSHA256    string    `json:"tree_sha256"`
	InstalledAt   time.Time `json:"installed_at"`
}

// Consent is the acceptance of a node: the license and the security notice that were shown, by version.
type Consent struct {
	LicenseID      string    `json:"license_id"`
	LicenseVersion string    `json:"license_version"`
	NoticeVersion  string    `json:"notice_version"`
	AcceptedAt     time.Time `json:"accepted_at"`
}

// Record is nodes.json: what is installed and what the user accepted, by node ID. It lies next to state.json of the model manager and is
// written on its own (the two are never written together).
type Record struct {
	Nodes    map[string]Installed `json:"nodes,omitempty"`
	Consents map[string]Consent   `json:"consents,omitempty"`
}

func (r Record) clone() Record {
	r.Nodes, r.Consents = maps.Clone(r.Nodes), maps.Clone(r.Consents)
	return r
}

// recordFile keeps the Record in a file, written atomically. A file that cannot be read is moved aside (nodes.json.corrupt) and the record
// starts empty: no node is then installed, so none is loaded, and an install checks everything again.
type recordFile struct {
	path   string
	mu     sync.Mutex
	loaded bool
	rec    Record
}

func (f *recordFile) load() {
	if f.loaded {
		return
	}
	f.loaded = true
	data, err := os.ReadFile(f.path)
	if err != nil {
		return
	}
	if json.Unmarshal(data, &f.rec) != nil {
		f.rec = Record{}
		_ = os.Rename(f.path, f.path+".corrupt")
	}
}

func (f *recordFile) get() Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	return f.rec.clone()
}

// update changes the record with edit and writes it; nothing changes when the write fails.
func (f *recordFile) update(edit func(*Record)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	next := f.rec.clone()
	edit(&next)
	data, err := json.MarshalIndent(next, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o750); err != nil {
		return fmt.Errorf("write %s: %w", f.path, err)
	}
	if err := fsutil.WriteFileAtomic(f.path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", f.path, err)
	}
	f.rec = next
	return nil
}

// Consent is the recorded acceptance of the node (false: none).
func (m *Manager) Consent(id string) (Consent, bool) {
	c, ok := m.record().Consents[id]
	return c, ok
}

// Accept records the acceptance of a node.
func (m *Manager) Accept(id string, c Consent) error {
	return m.rec.update(func(r *Record) {
		if r.Consents == nil {
			r.Consents = map[string]Consent{}
		}
		r.Consents[id] = c
	})
}
