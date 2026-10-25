package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// EngineRecord is the installed engine.
type EngineRecord struct {
	Version     string    `json:"version"`
	Variant     string    `json:"variant"`
	CPU         bool      `json:"cpu,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

// Consent is the acceptance of a license: the version that was shown.
type Consent struct {
	Version    string    `json:"version"`
	AcceptedAt time.Time `json:"accepted_at"`
}

// AdultContent records the age confirmation (18 or over, or of age where the user lives) and the agreement to the notice
// for adult models, with the version of the wording that was shown and when. Enabled is the setting "show adult models":
// turning it off keeps the confirmation (it is asked once), turning it on without a confirmation of the current wording
// is refused.
type AdultContent struct {
	Enabled       bool      `json:"enabled"`
	NoticeVersion string    `json:"notice_version"`
	ConfirmedAt   time.Time `json:"confirmed_at"`
}

// InstalledFile is a file of an installed model as it was verified.
type InstalledFile struct {
	Path   string `json:"path"` // models/<folder>/<name>
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// InstalledModel is a model whose files are all in place and verified. Workflows are the names of the models
// created on the bundled endpoint. Files are what was installed (the catalog of a later release may list others),
// and are what the reference count of a delete counts.
type InstalledModel struct {
	InstalledAt time.Time       `json:"installed_at"`
	Files       []InstalledFile `json:"files"`
	Workflows   []string        `json:"workflows"`
	LicenseID   string          `json:"license_id"`
}

// HFLicense is what the model card of a Hugging Face repository said of its license when a file was installed (a snapshot:
// the card may change later). Any field may be empty.
type HFLicense struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Link string `json:"link,omitempty"`
}

// UserModel is a file the user added from Hugging Face (spec model-manager.md §13): where it came from, the pins it was
// verified against, where it lies and what was created for it. It is kept by its Path, which is also the key in
// State.UserModels. Workflows are the names of the models created on the bundled endpoint (an all-in-one SDXL checkpoint).
type UserModel struct {
	Path        string    `json:"path"` // models/<folder>/<name>
	Repo        string    `json:"repo"`
	Commit      string    `json:"commit"`
	RepoPath    string    `json:"repo_path"`
	Folder      string    `json:"folder"`
	Name        string    `json:"name"`
	SHA256      string    `json:"sha256"`
	Size        int64     `json:"size"`
	License     HFLicense `json:"license"`
	Gated       bool      `json:"gated,omitempty"`
	NFAA        bool      `json:"nfaa,omitempty"`
	Family      string    `json:"family,omitempty"`
	Workflows   []string  `json:"workflows,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

// HFConsent is the user's confirmation of the terms of an import (the license shown and "the choice to fetch is yours, mam
// did not check the content") for one repository at one commit: TermsVersion is the wording that was shown.
type HFConsent struct {
	TermsVersion string    `json:"terms_version"`
	License      HFLicense `json:"license"`
	AcceptedAt   time.Time `json:"accepted_at"`
}

// State is the record of the model manager.
type State struct {
	// AutoStart starts the engine when mam starts (false: when it is first used).
	AutoStart bool `json:"auto_start"`
	// LastPort is the port the engine was last started on; it is tried first.
	LastPort int                       `json:"last_port,omitempty"`
	Engine   *EngineRecord             `json:"engine,omitempty"`
	Consents map[string]Consent        `json:"consents,omitempty"`
	Models   map[string]InstalledModel `json:"models,omitempty"`
	// AdultContent is the age confirmation and the "show adult models" setting (nil: never confirmed, adult models hidden).
	AdultContent *AdultContent `json:"adult_content,omitempty"`
	// UserModels are the files added from Hugging Face, by Path. HFConsents are the confirmations of the import terms, by
	// "<repo>@<commit>".
	UserModels map[string]UserModel `json:"user_models,omitempty"`
	HFConsents map[string]HFConsent `json:"hf_consents,omitempty"`
}

// clone copies the maps, so that a caller cannot change what the store holds.
func (s State) clone() State {
	s.Consents = maps.Clone(s.Consents)
	s.Models = maps.Clone(s.Models)
	s.UserModels = maps.Clone(s.UserModels)
	s.HFConsents = maps.Clone(s.HFConsents)
	if s.Engine != nil {
		e := *s.Engine
		s.Engine = &e
	}
	if s.AdultContent != nil {
		a := *s.AdultContent
		s.AdultContent = &a
	}
	return s
}

// Store keeps the State in Layout.StatePath, written atomically.
type Store struct {
	path string
	mu   sync.Mutex
	st   State
}

// CorruptSuffix is added to the name of a record that could not be read: it is moved aside, never deleted.
const CorruptSuffix = ".corrupt"

// Open reads the record (a missing file is the empty State). A record that cannot be read is moved aside to
// state.json.corrupt and the State starts empty, so that a damaged file never keeps mam from starting; the
// files of the models are still on disk and an install checks them again.
func Open(l Layout) (*Store, error) {
	s := &Store{path: l.StatePath()}
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	if err := json.Unmarshal(data, &s.st); err != nil {
		s.st = State{}
		if err := os.Rename(s.path, s.path+CorruptSuffix); err != nil {
			return nil, fmt.Errorf("move the unreadable %s aside: %w", s.path, err)
		}
	}
	return s, nil
}

// Get is a copy of the State.
func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.clone()
}

// Update changes the State with edit and writes it; nothing changes when the write fails.
func (s *Store) Update(edit func(*State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.st.clone()
	edit(&next)
	data, err := json.MarshalIndent(next, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	if err := fsutil.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	s.st = next
	return nil
}
