package jobhistory

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrBadCursor is the error of a cursor that List did not hand out.
var ErrBadCursor = errors.New("jobhistory: bad cursor")

// Filter selects the jobs List returns. Every field is optional; the conditions are combined with AND.
type Filter struct {
	Kind      string
	State     string
	ErrorCode string
	ProjectID string
	// Since is the oldest started_at returned (inclusive) and Until the newest (exclusive).
	Since, Until time.Time
	// After is the cursor of the last job of the previous page ("" for the first page).
	After string
	// Limit is the page size (at least 1).
	Limit int
}

// Page is one page of the newest-first listing.
type Page struct {
	Entries []Entry
	// Next is the cursor of the next page, "" on the last page.
	Next string
}

// List returns the jobs that match f, newest first (started_at, then id), one page at a time. The cursor
// is the position of the last job of the page (its started_at and id), so a job that is added or pruned
// while a client pages does not move the pages.
func (r *Repository) List(ctx context.Context, f Filter) (Page, error) {
	if f.Limit < 1 {
		return Page{}, errors.New("jobhistory: limit must be at least 1")
	}
	var (
		where []string
		args  []any
	)
	add := func(cond string, a ...any) { where, args = append(where, cond), append(args, a...) }
	if f.Kind != "" {
		add(`kind = ?`, f.Kind)
	}
	if f.State != "" {
		add(`state = ?`, f.State)
	}
	if f.ErrorCode != "" {
		add(`error_code = ?`, f.ErrorCode)
	}
	if f.ProjectID != "" {
		add(`project_id = ?`, f.ProjectID)
	}
	if !f.Since.IsZero() {
		add(`started_at >= ?`, repository.FormatTime(f.Since))
	}
	if !f.Until.IsZero() {
		add(`started_at < ?`, repository.FormatTime(f.Until))
	}
	if f.After != "" {
		startedAt, id, err := decodeCursor(f.After)
		if err != nil {
			return Page{}, err
		}
		add(`(started_at < ? OR (started_at = ? AND id < ?))`, startedAt, startedAt, id)
	}
	query := `SELECT ` + columns + ` FROM mcp_job_history`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY started_at DESC, id DESC LIMIT ?`
	entries, err := r.query(ctx, query, append(args, f.Limit+1)...)
	if err != nil {
		return Page{}, err
	}
	page := Page{Entries: entries}
	if len(entries) > f.Limit {
		page.Entries = entries[:f.Limit]
		page.Next = encodeCursor(page.Entries[f.Limit-1])
	}
	return page, nil
}

func encodeCursor(e Entry) string {
	return base64.RawURLEncoding.EncodeToString([]byte(repository.FormatTime(e.StartedAt) + "|" + e.ID))
}

func decodeCursor(s string) (startedAt, id string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrBadCursor, err)
	}
	startedAt, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return "", "", ErrBadCursor
	}
	if _, err := repository.ParseTime(startedAt); err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrBadCursor, err)
	}
	return startedAt, id, nil
}
