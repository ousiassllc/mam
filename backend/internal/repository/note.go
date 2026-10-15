package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// NoteRepository persists Notes (F-31, spec/architecture/er.md §2.5): the memos of a Project or of a
// Series, their append-only versions, and the World State promotions proposed for the versions of
// Project memos. All writes go through DB.Write.
type NoteRepository struct {
	db    *DB
	now   func() time.Time
	world *WorldRepository
}

// NewNoteRepository returns a NoteRepository backed by db.
func NewNoteRepository(db *DB) *NoteRepository {
	return &NoteRepository{db: db, now: time.Now, world: NewWorldRepository(db)}
}

// NewNote is a memo to create: of the Project ProjectID or of the Series SeriesID (exactly one).
type NewNote struct {
	ProjectID string
	SeriesID  string
	Category  domain.NoteCategory
	Pinned    bool
	Title     string
	Content   string
	Source    domain.NoteSource
}

// NoteUpdate is a change of a memo; nil fields stay as they are. A different Title or Content
// appends a note_version (with Source); Category, Pinned and Archived update the row.
type NoteUpdate struct {
	Title    *string
	Content  *string
	Category *domain.NoteCategory
	Pinned   *bool
	Archived *bool
	Source   domain.NoteSource
}

const noteSelect = `SELECT n.id, COALESCE(n.project_id, ''), COALESCE(n.series_id, ''), COALESCE(n.origin_id, ''), n.category, n.pinned, n.archived_at, n.created_at,
		v.id, v.version_number, v.title, v.content, v.source, v.created_at
	FROM note n JOIN note_version v ON v.note_id = n.id
	 AND v.version_number = (SELECT MAX(version_number) FROM note_version WHERE note_id = n.id)`

func scanNoteView(row rowScanner) (domain.NoteView, error) {
	var (
		v                             domain.NoteView
		pinned                        int
		archived                      sql.NullString
		createdAt, versionAt          string
		noteID, projectID, seriesID   string
		originID                      string
		category, versionID, versionS string
	)
	if err := row.Scan(&noteID, &projectID, &seriesID, &originID, &category, &pinned, &archived, &createdAt,
		&versionID, &v.Version.Number, &v.Version.Title, &v.Version.Content, &versionS, &versionAt); err != nil {
		return v, err
	}
	v.ID, v.ProjectID, v.SeriesID, v.OriginID = noteID, projectID, seriesID, originID
	v.Category, v.Pinned = domain.NoteCategory(category), pinned == 1
	v.Version.ID, v.Version.NoteID, v.Version.Source = versionID, noteID, domain.NoteSource(versionS)
	var err error
	if v.CreatedAt, err = ParseTime(createdAt); err != nil {
		return v, err
	}
	if v.Version.CreatedAt, err = ParseTime(versionAt); err != nil {
		return v, err
	}
	if archived.Valid {
		t, err := ParseTime(archived.String)
		if err != nil {
			return v, err
		}
		v.ArchivedAt = &t
	}
	return v, nil
}

func readNote(ctx context.Context, q queryer, id string) (domain.NoteView, error) {
	v, err := scanNoteView(q.QueryRowContext(ctx, noteSelect+` WHERE n.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, fmt.Errorf("get note: %w", err)
	}
	return v, nil
}

// Get returns the note with its latest version (ErrNotFound for an unknown note).
func (r *NoteRepository) Get(ctx context.Context, id string) (domain.NoteView, error) {
	return readNote(ctx, r.db.Reader(), id)
}

// GetVersion returns the note as it was at version number (Version is that version, not the latest).
// It returns ErrNotFound for an unknown note or version.
func (r *NoteRepository) GetVersion(ctx context.Context, id string, number int) (domain.NoteView, error) {
	v, err := r.Get(ctx, id)
	if err != nil {
		return v, err
	}
	var at string
	v.Version.Number = number
	err = r.db.Reader().QueryRowContext(ctx,
		`SELECT id, title, content, source, created_at FROM note_version WHERE note_id = ? AND version_number = ?`, id, number).
		Scan(&v.Version.ID, &v.Version.Title, &v.Version.Content, &v.Version.Source, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NoteView{}, ErrNotFound
	}
	if err != nil {
		return domain.NoteView{}, fmt.Errorf("get note version: %w", err)
	}
	if v.Version.CreatedAt, err = ParseTime(at); err != nil {
		return domain.NoteView{}, err
	}
	return v, nil
}

// GetWithHistory is Get with the version history (newest first).
func (r *NoteRepository) GetWithHistory(ctx context.Context, id string) (domain.NoteView, error) {
	v, err := r.Get(ctx, id)
	if err != nil {
		return v, err
	}
	hist, err := noteHistories(ctx, r.db.Reader(), `note_id = ?`, id)
	if err != nil {
		return v, err
	}
	v.Versions = hist[v.ID]
	return v, nil
}

// ListProject returns every note of the Project, archived ones included, each with its history:
// pinned first, then the most recently changed first (ErrNotFound for an unknown Project).
func (r *NoteRepository) ListProject(ctx context.Context, projectID string) ([]domain.NoteView, error) {
	if err := Exists(ctx, r.db.Reader(), `SELECT 1 FROM project WHERE id = ?`, projectID); err != nil {
		return nil, err
	}
	return listNotes(ctx, r.db.Reader(), `n.project_id = ?`, projectID, true)
}

// ListSeries returns every canonical note of the Series like ListProject (ErrNotFound for an
// unknown Series).
func (r *NoteRepository) ListSeries(ctx context.Context, seriesID string) ([]domain.NoteView, error) {
	if err := Exists(ctx, r.db.Reader(), `SELECT 1 FROM series WHERE id = ?`, seriesID); err != nil {
		return nil, err
	}
	return listNotes(ctx, r.db.Reader(), `n.series_id = ?`, seriesID, true)
}

// Context returns the notes that Writer, Director and Prompt Planner may be given for the Project:
// the active (not archived) ones with their latest version. Choosing among them is
// domain.SelectNotes.
func (r *NoteRepository) Context(ctx context.Context, projectID string) ([]domain.NoteView, error) {
	return listNotes(ctx, r.db.Reader(), `n.project_id = ? AND n.archived_at IS NULL`, projectID, false)
}

func listNotes(ctx context.Context, q queryer, where, id string, withHistory bool) ([]domain.NoteView, error) {
	out, err := queryAll(ctx, q, scanNoteView, noteSelect+` WHERE `+where+` ORDER BY n.pinned DESC, v.created_at DESC, n.id`, id) //nolint:gosec // where is a constant of this file
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	if !withHistory || len(out) == 0 {
		return out, nil
	}
	hist, err := noteHistories(ctx, q, `note_id IN (SELECT n.id FROM note n WHERE `+where+`)`, id)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Versions = hist[out[i].ID]
	}
	return out, nil
}

// noteHistories reads the versions (newest first) of the notes that where selects, by note ID.
func noteHistories(ctx context.Context, q queryer, where, id string) (map[string][]domain.VersionEntry, error) {
	type noteVersion struct {
		noteID string
		domain.VersionEntry
	}
	rows, err := queryAll(ctx, q, func(row rowScanner) (noteVersion, error) {
		var (
			v  noteVersion
			at string
		)
		if err := row.Scan(&v.noteID, &v.Number, &v.Summary, &at); err != nil {
			return v, err
		}
		var err error
		v.CreatedAt, err = ParseTime(at)
		return v, err
	}, `SELECT note_id, version_number, title, created_at FROM note_version WHERE `+where+` ORDER BY note_id, version_number DESC`, id) //nolint:gosec // where is built from constants of this file
	if err != nil {
		return nil, fmt.Errorf("list note versions: %w", err)
	}
	out := map[string][]domain.VersionEntry{}
	for _, v := range rows {
		out[v.noteID] = append(out[v.noteID], v.VersionEntry)
	}
	return out, nil
}
