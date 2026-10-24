// Package tokenstore keeps the ChatGPT sign-in of one login system in the OS secure storage (credential.Store).
//
// A sign-in is several entries, one per field (the access token, the refresh token, the ID token and the
// account facts), never one JSON blob: an entry above the limit of the OS store (Windows: 2560 bytes) is split
// transparently. Nothing here reaches the data directory, the log or an error message.
package tokenstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

// ErrNotSignedIn means no sign-in is stored.
var ErrNotSignedIn = errors.New("tokenstore: not signed in")

// Meta is what is known about a sign-in besides its tokens. It holds no secret.
type Meta struct {
	// ClientID is the OAuth client the tokens belong to (the issued oaiapp_… id for the official login).
	ClientID  string    `json:"client_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Scopes    []string  `json:"scopes,omitempty"`
	Email     string    `json:"email,omitempty"`
	// Plan is the ChatGPT plan type when the ID token says it ("plus", "pro", …).
	Plan      string `json:"plan,omitempty"`
	AccountID string `json:"account_id,omitempty"`
	Subject   string `json:"subject,omitempty"`
	// ReloginRequired is set when the refresh token was refused for good (revoked, expired, reused): the
	// tokens stay, but only a new sign-in helps.
	ReloginRequired bool      `json:"relogin_required,omitempty"`
	ReloginReason   string    `json:"relogin_reason,omitempty"`
	SavedAt         time.Time `json:"saved_at"`
}

// Tokens is one stored sign-in.
type Tokens struct {
	Access  string
	Refresh string
	IDToken string
	Meta    Meta
}

// Registration is what survives a sign-out: the host id and the client id issued by the dynamic registration
// of the official login (SIWC: "Retain its account/client mapping and this host's ID for a later sign-in").
type Registration struct {
	HostID   string `json:"host_id"`
	ClientID string `json:"client_id,omitempty"`
}

// Fields of a sign-in. The names are part of the key (credential account "api-key:chatgpt/<ns>/<field>").
const (
	fieldAccess       = "access"
	fieldRefresh      = "refresh"
	fieldIDToken      = "id_token"
	fieldMeta         = "meta"
	fieldRegistration = "registration"
)

// Store is the sign-in of one login system, under the namespace ns ("official", "codex").
type Store struct {
	kv kv
	ns string
}

// New returns the Store of namespace ns over s.
func New(s credential.Store, ns string) *Store { return &Store{kv: kv{s}, ns: ns} }

func (s *Store) key(field string) string { return "chatgpt/" + s.ns + "/" + field }

// Save replaces the stored sign-in. The refresh token goes first (the one field that cannot be got again), the
// metadata last: a crash in between leaves a metadata that is older than the tokens, so the next call refreshes early.
func (s *Store) Save(t Tokens) error {
	if t.Access == "" || t.Refresh == "" {
		return errors.New("tokenstore: access and refresh tokens are required")
	}
	meta, err := json.Marshal(t.Meta)
	if err != nil {
		return fmt.Errorf("tokenstore: encode metadata: %w", err)
	}
	for _, f := range []struct{ field, value string }{
		{fieldRefresh, t.Refresh}, {fieldIDToken, t.IDToken}, {fieldAccess, t.Access}, {fieldMeta, string(meta)},
	} {
		if f.value == "" {
			if err := s.kv.delete(s.key(f.field)); err != nil {
				return fmt.Errorf("tokenstore: clear %s: %w", f.field, err)
			}
			continue
		}
		if err := s.kv.put(s.key(f.field), f.value); err != nil {
			return fmt.Errorf("tokenstore: store %s: %w", f.field, err)
		}
	}
	return nil
}

// Load returns the stored sign-in, or ErrNotSignedIn.
func (s *Store) Load() (Tokens, error) {
	raw, err := s.kv.get(s.key(fieldMeta))
	if errors.Is(err, credential.ErrNotFound) {
		return Tokens{}, ErrNotSignedIn
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("tokenstore: load metadata: %w", err)
	}
	var t Tokens
	if err := json.Unmarshal([]byte(raw), &t.Meta); err != nil {
		return Tokens{}, ErrNotSignedIn // an entry that is not ours: treated as not signed in, the next sign-in replaces it
	}
	for _, f := range []struct {
		field string
		into  *string
		need  bool
	}{{fieldAccess, &t.Access, true}, {fieldRefresh, &t.Refresh, true}, {fieldIDToken, &t.IDToken, false}} {
		v, err := s.kv.get(s.key(f.field))
		switch {
		case errors.Is(err, credential.ErrNotFound) && !f.need:
		case errors.Is(err, credential.ErrNotFound):
			return Tokens{}, ErrNotSignedIn
		case err != nil:
			return Tokens{}, fmt.Errorf("tokenstore: load %s: %w", f.field, err)
		default:
			*f.into = v
		}
	}
	return t, nil
}

// SaveMeta replaces the metadata only (the re-login flag changes it without new tokens).
func (s *Store) SaveMeta(m Meta) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("tokenstore: encode metadata: %w", err)
	}
	return s.kv.put(s.key(fieldMeta), string(raw))
}

// Clear deletes the sign-in (not the Registration). Every field is tried; the first failure is returned.
func (s *Store) Clear() error {
	var errs []error
	// The metadata goes first: Load then reports "not signed in" even if a later delete fails.
	for _, f := range []string{fieldMeta, fieldRefresh, fieldAccess, fieldIDToken} {
		if err := s.kv.delete(s.key(f)); err != nil {
			errs = append(errs, fmt.Errorf("tokenstore: delete %s: %w", f, err))
		}
	}
	return errors.Join(errs...)
}

// Registration returns the stored registration; the zero value when there is none.
func (s *Store) Registration() (Registration, error) {
	raw, err := s.kv.get(s.key(fieldRegistration))
	if errors.Is(err, credential.ErrNotFound) {
		return Registration{}, nil
	}
	if err != nil {
		return Registration{}, fmt.Errorf("tokenstore: load registration: %w", err)
	}
	var r Registration
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return Registration{}, nil
	}
	return r, nil
}

// SaveRegistration stores the registration.
func (s *Store) SaveRegistration(r Registration) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("tokenstore: encode registration: %w", err)
	}
	return s.kv.put(s.key(fieldRegistration), string(raw))
}
