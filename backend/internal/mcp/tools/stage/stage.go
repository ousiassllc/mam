// Package stage keeps the files of a ticket upload until the tool that uses them is called. The screen
// sends the message and its attachments in one request; over MCP the files arrive first (a ticket
// upload, see mcpkit.Files) and the message follows, referring to them by upload_id. The file is checked by
// service/upload.go when the message is posted, exactly as a file of the screen is.
package stage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Field is the input field of the tools that take staged uploads; a bad id is a ValidationError of it.
const Field = "attachment_upload_ids"

// File is one staged upload.
type File struct {
	ID        string    `json:"upload_id"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	ExpiresAt time.Time `json:"expires_at"`

	path  string
	owner string
}

// Store holds staged uploads for mcpkit.TicketTTL. They live in the temporary directory of the OS, not
// in the assets directory: only a message or Project that takes them makes them assets.
type Store struct {
	ttl time.Duration
	now func() time.Time

	mu    sync.Mutex
	dir   string
	files map[string]File
}

// New returns an empty Store.
func New() *Store {
	return &Store{ttl: mcpkit.TicketTTL, now: time.Now, files: map[string]File{}}
}

// Add stores the uploaded file for owner (the Project it is for; "" for a file not tied to one).
func (s *Store) Add(owner string, in mcpkit.UploadInput) (File, error) {
	src, err := in.Header.Open()
	if err != nil {
		return File{}, err
	}
	defer func() { _ = src.Close() }()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if s.dir == "" {
		if s.dir, err = os.MkdirTemp("", "mam-mcp-stage-"); err != nil {
			return File{}, fmt.Errorf("create staging directory: %w", err)
		}
	}
	dst, err := os.CreateTemp(s.dir, "upload-*")
	if err != nil {
		return File{}, fmt.Errorf("stage upload: %w", err)
	}
	size, copyErr := io.Copy(dst, src)
	if closeErr := dst.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(dst.Name())
		return File{}, fmt.Errorf("stage upload: %w", copyErr)
	}
	f := File{
		ID: domain.NewID(), Name: filepath.Base(in.Header.Filename), SizeBytes: size,
		ExpiresAt: s.now().Add(s.ttl), path: dst.Name(), owner: owner,
	}
	s.files[f.ID] = f
	return f, nil
}

// Claim takes the files with the ids out of the Store, in the order of ids (a repeated id counts once). An
// id that is unknown, expired, already taken or staged for another owner is a ValidationError and nothing
// is taken. The caller either Discards the files (they were used) or Releases them (the call failed).
func (s *Store) Claim(owner string, ids []string) ([]File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	var claimed []File
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		f, ok := s.files[id]
		if !ok || f.owner != owner {
			return nil, &service.ValidationError{Field: Field, Message: "アップロードが見つかりません（期限切れか、使用済みか、別のプロジェクト用です）: " + id}
		}
		claimed = append(claimed, f)
	}
	for _, f := range claimed {
		delete(s.files, f.ID)
	}
	return claimed, nil
}

// Release puts claimed files back, after the call that claimed them failed, so that the client can retry.
func (s *Store) Release(files []File) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range files {
		s.files[f.ID] = f
	}
}

// Discard deletes claimed files that were used.
func (s *Store) Discard(files []File) {
	for _, f := range files {
		_ = os.Remove(f.path)
	}
}

// Uploads are the files as the service layer takes them.
func Uploads(files []File) []service.Upload {
	uploads := make([]service.Upload, len(files))
	for i, f := range files {
		uploads[i] = service.Upload{Name: f.Name, Size: f.SizeBytes, Open: func() (io.ReadCloser, error) { return os.Open(f.path) }}
	}
	return uploads
}

// sweep deletes the expired files. The caller holds mu.
func (s *Store) sweep() {
	now := s.now()
	for id, f := range s.files {
		if now.After(f.ExpiresAt) {
			_ = os.Remove(f.path)
			delete(s.files, id)
		}
	}
}
