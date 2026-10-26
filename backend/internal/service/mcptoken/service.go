// Package mcptoken issues, lists, revokes and verifies the bearer tokens of the MCP server
// (spec/architecture/mcp.md). A token value is 32 random bytes, shown once when it is issued; the database
// holds only its SHA-256, so a stolen database cannot be used to connect.
package mcptoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	repo "github.com/ousiassllc/mam/backend/internal/repository/mcptoken"
)

const (
	// ValuePrefix starts every token value, so that a leaked value is recognisable (secret scanners).
	ValuePrefix = "mam_mcp_"
	// MaxNameRunes bounds the name of a token.
	MaxNameRunes = 64
	valueBytes   = 32
	// DisplayPrefixLen is how many characters of the random part are kept for display.
	DisplayPrefixLen = 8
	// touchInterval is the shortest time between two writes of last_used_at of one token.
	touchInterval = time.Minute
)

// ErrNotFound means the token to revoke does not exist.
var ErrNotFound = errors.New("mcptoken: not found")

// InvalidError is a name the service refuses. It has the shape of service.ValidationError, which this package
// cannot use: service.Services holds the token service, so service imports this package.
type InvalidError struct{ Field, Message string }

func (e *InvalidError) Error() string { return "invalid input: " + e.Field + ": " + e.Message }

// ErrInvalidToken means a presented value is empty, unknown or revoked. The three are not told apart.
var ErrInvalidToken = errors.New("mcptoken: invalid token")

// Token is one issued token as the settings screen lists it. It never carries the value.
type Token = repo.Token

// Issued is a new token with its value. The value exists only here: it is not stored.
type Issued struct {
	Token Token
	Value string
}

// Service manages the MCP bearer tokens.
type Service struct {
	repo *repo.Repository
	now  func() time.Time
	rand io.Reader

	mu      sync.Mutex
	touched map[string]time.Time // id -> when last_used_at was last written
}

// New returns a Service over r.
func New(r *repo.Repository) *Service {
	return &Service{repo: r, now: time.Now, rand: rand.Reader, touched: map[string]time.Time{}}
}

// Issue creates a token called name (1 to MaxNameRunes characters) and returns it with its value.
func (s *Service) Issue(ctx context.Context, name string) (Issued, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return Issued{}, &InvalidError{Field: "name", Message: "トークンの名前を入力してください"}
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return Issued{}, &InvalidError{Field: "name", Message: "名前は 64 文字以内で入力してください"}
	}
	raw := make([]byte, valueBytes)
	if _, err := io.ReadFull(s.rand, raw); err != nil {
		return Issued{}, err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	value := ValuePrefix + secret
	t := Token{ID: domain.NewID(), Name: name, Prefix: secret[:DisplayPrefixLen], CreatedAt: s.now().UTC()}
	if err := s.repo.Create(ctx, t, hashOf(value)); err != nil {
		return Issued{}, err
	}
	return Issued{Token: t, Value: value}, nil
}

// List returns every token, newest first.
func (s *Service) List(ctx context.Context) ([]Token, error) { return s.repo.List(ctx) }

// Revoke ends the token: every later request with it is refused. ErrNotFound when it does not exist.
func (s *Service) Revoke(ctx context.Context, id string) error {
	err := s.repo.Revoke(ctx, id, s.now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// Verify returns the active token whose value is value, or ErrInvalidToken. A successful check records the
// use (last_used_at), at most once a minute per token.
func (s *Service) Verify(ctx context.Context, value string) (Token, error) {
	if !strings.HasPrefix(value, ValuePrefix) {
		return Token{}, ErrInvalidToken
	}
	t, err := s.repo.GetByHash(ctx, hashOf(value))
	if errors.Is(err, repository.ErrNotFound) {
		return Token{}, ErrInvalidToken
	}
	if err != nil {
		return Token{}, err
	}
	if t.Revoked() {
		return Token{}, ErrInvalidToken
	}
	s.touch(ctx, t.ID)
	return t, nil
}

// Active reports whether the token exists and is not revoked.
func (s *Service) Active(ctx context.Context, id string) bool {
	t, err := s.repo.Get(ctx, id)
	return err == nil && !t.Revoked()
}

// touch writes last_used_at unless it was written within touchInterval. A failed write is not a failed
// request: the next use tries again.
func (s *Service) touch(ctx context.Context, id string) {
	now := s.now().UTC()
	s.mu.Lock()
	last, seen := s.touched[id]
	if seen && now.Sub(last) < touchInterval {
		s.mu.Unlock()
		return
	}
	s.touched[id] = now
	s.mu.Unlock()
	if err := s.repo.TouchUsed(context.WithoutCancel(ctx), id, now); err != nil {
		s.mu.Lock()
		delete(s.touched, id)
		s.mu.Unlock()
	}
}

func hashOf(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
