package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// StateFile is the record of mam's own settings for the Codex login, in the data directory. It holds no token: the
// path of a codex the user set, and the facts of the account as last read (so that the settings screen can show them
// without starting the app-server).
const StateFile = "chatgpt-codex.json"

// AccountView is what is known of the signed-in account. It holds no token.
type AccountView struct {
	SignedIn bool   `json:"signed_in"`
	Type     string `json:"type,omitempty"`
	Email    string `json:"email,omitempty"`
	PlanType string `json:"plan_type,omitempty"`
	// CheckedAt is when the app-server last said so (zero: never).
	CheckedAt time.Time `json:"checked_at,omitempty"`
}

type stateData struct {
	ExePath string      `json:"exe_path,omitempty"`
	Account AccountView `json:"account"`
}

// stateStore keeps the record in a file, written atomically.
type stateStore struct {
	path string
	mu   sync.Mutex
	d    stateData
}

func openState(path string) *stateStore {
	s := &stateStore{path: path}
	if b, err := os.ReadFile(path); err == nil { // #nosec G304 -- mam's own file
		_ = json.Unmarshal(b, &s.d) // a record that cannot be read is the empty one: it is only a cache
	}
	return s
}

func (s *stateStore) get() stateData {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d
}

func (s *stateStore) update(f func(*stateData)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.d
	f(&next)
	b, err := json.MarshalIndent(next, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(s.path, b, 0o600); err != nil {
		return err
	}
	s.d = next
	return nil
}

func (s *stateStore) remove() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d = stateData{}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
