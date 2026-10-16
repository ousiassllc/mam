package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Create makes a memo with its first version. It returns ErrNotFound when the Project or Series
// does not exist.
func (r *NoteRepository) Create(ctx context.Context, n NewNote) (domain.NoteView, error) {
	if (n.ProjectID == "") == (n.SeriesID == "") {
		return domain.NoteView{}, errors.New("create note: exactly one of project and series is required")
	}
	if n.Category == "" {
		n.Category = domain.NoteCategoryOther
	}
	id := domain.NewID()
	now := FormatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if n.ProjectID != "" {
			if err := Exists(ctx, tx, `SELECT 1 FROM project WHERE id = ?`, n.ProjectID); err != nil {
				return err
			}
		} else if err := Exists(ctx, tx, `SELECT 1 FROM series WHERE id = ?`, n.SeriesID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO note (id, project_id, series_id, category, pinned, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, nullIfEmpty(n.ProjectID), nullIfEmpty(n.SeriesID), n.Category, boolInt(n.Pinned), now); err != nil {
			return fmt.Errorf("insert note: %w", err)
		}
		return insertNoteVersion(ctx, tx, id, 1, n.Title, n.Content, n.Source, now)
	})
	if errors.Is(err, ErrNotFound) {
		return domain.NoteView{}, err
	}
	if err != nil {
		return domain.NoteView{}, fmt.Errorf("create note: %w", err)
	}
	return r.Get(ctx, id)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func insertNoteVersion(ctx context.Context, tx *sql.Tx, noteID string, number int, title, content string, source domain.NoteSource, now string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO note_version (id, note_id, version_number, title, content, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		domain.NewID(), noteID, number, title, content, source, now); err != nil {
		return fmt.Errorf("insert note version: %w", err)
	}
	return nil
}

// appendNoteVersion appends a version to the note after its latest one.
func appendNoteVersion(ctx context.Context, tx *sql.Tx, noteID, title, content string, source domain.NoteSource, now string) error {
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM note_version WHERE note_id = ?`, noteID).Scan(&next); err != nil {
		return err
	}
	return insertNoteVersion(ctx, tx, noteID, next, title, content, source, now)
}

// Update changes a memo and returns it. newVersion reports whether the title or content changed and
// a version was appended. It returns ErrNotFound for an unknown note.
func (r *NoteRepository) Update(ctx context.Context, id string, u NoteUpdate) (domain.NoteView, bool, error) {
	newVersion := false
	now := FormatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		cur, err := readNote(ctx, tx, id)
		if err != nil {
			return err
		}
		title, content := cur.Version.Title, cur.Version.Content
		if u.Title != nil {
			title = *u.Title
		}
		if u.Content != nil {
			content = *u.Content
		}
		if title != cur.Version.Title || content != cur.Version.Content {
			if err := appendNoteVersion(ctx, tx, id, title, content, u.Source, now); err != nil {
				return err
			}
			newVersion = true
		}
		return updateNoteRow(ctx, tx, cur, u, now)
	})
	if errors.Is(err, ErrNotFound) {
		return domain.NoteView{}, false, err
	}
	if err != nil {
		return domain.NoteView{}, false, fmt.Errorf("update note: %w", err)
	}
	v, err := r.Get(ctx, id)
	return v, newVersion, err
}

func updateNoteRow(ctx context.Context, tx *sql.Tx, cur domain.NoteView, u NoteUpdate, now string) error {
	if u.Category != nil && *u.Category != cur.Category {
		if _, err := tx.ExecContext(ctx, `UPDATE note SET category = ? WHERE id = ?`, *u.Category, cur.ID); err != nil {
			return err
		}
	}
	if u.Pinned != nil && *u.Pinned != cur.Pinned {
		if _, err := tx.ExecContext(ctx, `UPDATE note SET pinned = ? WHERE id = ?`, boolInt(*u.Pinned), cur.ID); err != nil {
			return err
		}
	}
	switch {
	case u.Archived == nil:
	case *u.Archived && !cur.Archived():
		_, err := tx.ExecContext(ctx, `UPDATE note SET archived_at = ? WHERE id = ?`, now, cur.ID)
		return err
	case !*u.Archived && cur.Archived():
		_, err := tx.ExecContext(ctx, `UPDATE note SET archived_at = NULL WHERE id = ?`, cur.ID)
		return err
	}
	return nil
}

// Restore appends the title and content of version number as the newest version of the note and
// returns the note (F-28). It returns ErrNotFound for an unknown note or version.
func (r *NoteRepository) Restore(ctx context.Context, id string, number int) (domain.NoteView, error) {
	now := FormatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var title, content string
		var source domain.NoteSource
		err := tx.QueryRowContext(ctx, `SELECT title, content, source FROM note_version WHERE note_id = ? AND version_number = ?`, id, number).Scan(&title, &content, &source)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return appendNoteVersion(ctx, tx, id, title, content, source, now)
	})
	if errors.Is(err, ErrNotFound) {
		return domain.NoteView{}, err
	}
	if err != nil {
		return domain.NoteView{}, fmt.Errorf("restore note: %w", err)
	}
	return r.Get(ctx, id)
}
