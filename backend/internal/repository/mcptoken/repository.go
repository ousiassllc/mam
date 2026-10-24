// Package mcptoken is the repository of the bearer tokens of the MCP server (table mcp_token). It sits
// beside the per-aggregate repositories of the parent package and uses its DB and its error values.
// Only the SHA-256 of a token value is stored (see service/mcptoken).
package mcptoken

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Token is one row of mcp_token. It never carries the token value, only its display prefix.
type Token struct {
	ID         string
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time // nil: never used
	RevokedAt  *time.Time // nil: active
}

// Revoked reports whether the token was revoked.
func (t Token) Revoked() bool { return t.RevokedAt != nil }

// Repository persists the MCP tokens. All writes go through DB.Write.
type Repository struct{ db *repository.DB }

// New returns a Repository backed by db.
func New(db *repository.DB) *Repository { return &Repository{db: db} }

const selectRow = `SELECT id, name, token_prefix, created_at, last_used_at, revoked_at FROM mcp_token`

// Create inserts t with the SHA-256 hex digest of its value.
func (r *Repository) Create(ctx context.Context, t Token, hash string) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO mcp_token (id, name, token_hash, token_prefix, created_at) VALUES (?, ?, ?, ?, ?)`,
			t.ID, t.Name, hash, t.Prefix, repository.FormatTime(t.CreatedAt))
		if err != nil {
			return fmt.Errorf("insert mcp token: %w", err)
		}
		return nil
	})
}

// List returns every token, newest first.
func (r *Repository) List(ctx context.Context) ([]Token, error) {
	rows, err := r.db.Reader().QueryContext(ctx, selectRow+` ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return nil, fmt.Errorf("list mcp tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Token
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetByHash returns the token whose value has the SHA-256 hex digest hash, or repository.ErrNotFound.
func (r *Repository) GetByHash(ctx context.Context, hash string) (Token, error) {
	return r.get(ctx, selectRow+` WHERE token_hash = ?`, hash)
}

// Get returns the token with the id, or repository.ErrNotFound.
func (r *Repository) Get(ctx context.Context, id string) (Token, error) {
	return r.get(ctx, selectRow+` WHERE id = ?`, id)
}

func (r *Repository) get(ctx context.Context, query string, arg string) (Token, error) {
	t, err := scan(r.db.Reader().QueryRowContext(ctx, query, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, repository.ErrNotFound
	}
	return t, err
}

// Revoke marks the token revoked at at. Revoking a revoked token keeps the first time and is not an
// error; a token that does not exist is repository.ErrNotFound.
func (r *Repository) Revoke(ctx context.Context, id string, at time.Time) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE mcp_token SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, repository.FormatTime(at), id)
		if err != nil {
			return fmt.Errorf("revoke mcp token: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("revoke mcp token: %w", err)
		} else if n == 0 {
			return repository.ErrNotFound
		}
		return nil
	})
}

// TouchUsed records that the token was used at at.
func (r *Repository) TouchUsed(ctx context.Context, id string, at time.Time) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE mcp_token SET last_used_at = ? WHERE id = ?`, repository.FormatTime(at), id); err != nil {
			return fmt.Errorf("touch mcp token: %w", err)
		}
		return nil
	})
}

type rowScanner interface{ Scan(dest ...any) error }

func scan(s rowScanner) (Token, error) {
	var (
		t                      Token
		created                string
		lastUsed, revokedAtRaw sql.NullString
	)
	if err := s.Scan(&t.ID, &t.Name, &t.Prefix, &created, &lastUsed, &revokedAtRaw); err != nil {
		return Token{}, err
	}
	var err error
	if t.CreatedAt, err = repository.ParseTime(created); err != nil {
		return Token{}, err
	}
	if t.LastUsedAt, err = parseNull(lastUsed); err != nil {
		return Token{}, err
	}
	if t.RevokedAt, err = parseNull(revokedAtRaw); err != nil {
		return Token{}, err
	}
	return t, nil
}

func parseNull(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := repository.ParseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
