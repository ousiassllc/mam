package repository

import (
	"context"
	"database/sql"
	"encoding/json"
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
	if v.CreatedAt, err = parseTime(createdAt); err != nil {
		return v, err
	}
	if v.Version.CreatedAt, err = parseTime(versionAt); err != nil {
		return v, err
	}
	if archived.Valid {
		t, err := parseTime(archived.String)
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
	if v.Version.CreatedAt, err = parseTime(at); err != nil {
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
	if err := exists(ctx, r.db.Reader(), `SELECT 1 FROM project WHERE id = ?`, projectID); err != nil {
		return nil, err
	}
	return listNotes(ctx, r.db.Reader(), `n.project_id = ?`, projectID, true)
}

// ListSeries returns every canonical note of the Series like ListProject (ErrNotFound for an
// unknown Series).
func (r *NoteRepository) ListSeries(ctx context.Context, seriesID string) ([]domain.NoteView, error) {
	if err := exists(ctx, r.db.Reader(), `SELECT 1 FROM series WHERE id = ?`, seriesID); err != nil {
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
	rows, err := q.QueryContext(ctx, noteSelect+` WHERE `+where+` ORDER BY n.pinned DESC, v.created_at DESC, n.id`, id) //nolint:gosec // where is a constant of this file
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.NoteView
	for rows.Next() {
		v, err := scanNoteView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
	rows, err := q.QueryContext(ctx, `SELECT note_id, version_number, title, created_at FROM note_version WHERE `+where+` ORDER BY note_id, version_number DESC`, id) //nolint:gosec // where is built from constants of this file
	if err != nil {
		return nil, fmt.Errorf("list note versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]domain.VersionEntry{}
	for rows.Next() {
		var (
			noteID, at string
			e          domain.VersionEntry
		)
		if err := rows.Scan(&noteID, &e.Number, &e.Summary, &at); err != nil {
			return nil, err
		}
		if e.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out[noteID] = append(out[noteID], e)
	}
	return out, rows.Err()
}

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
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if n.ProjectID != "" {
			if err := exists(ctx, tx, `SELECT 1 FROM project WHERE id = ?`, n.ProjectID); err != nil {
				return err
			}
		} else if err := exists(ctx, tx, `SELECT 1 FROM series WHERE id = ?`, n.SeriesID); err != nil {
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

// appendNoteVersion appends a version to the note after its latest one and returns its number.
func appendNoteVersion(ctx context.Context, tx *sql.Tx, noteID, title, content string, source domain.NoteSource, now string) (int, error) {
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM note_version WHERE note_id = ?`, noteID).Scan(&next); err != nil {
		return 0, err
	}
	return next, insertNoteVersion(ctx, tx, noteID, next, title, content, source, now)
}

// Update changes a memo and returns it. newVersion reports whether the title or content changed and
// a version was appended. It returns ErrNotFound for an unknown note.
func (r *NoteRepository) Update(ctx context.Context, id string, u NoteUpdate) (domain.NoteView, bool, error) {
	newVersion := false
	now := formatTime(r.now())
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
			if _, err := appendNoteVersion(ctx, tx, id, title, content, u.Source, now); err != nil {
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
	now := formatTime(r.now())
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
		_, err = appendNoteVersion(ctx, tx, id, title, content, source, now)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return domain.NoteView{}, err
	}
	if err != nil {
		return domain.NoteView{}, fmt.Errorf("restore note: %w", err)
	}
	return r.Get(ctx, id)
}

// copyNotes gives the Project (an Episode of the Series) its own memos: the latest version of every
// active canonical note of the Series becomes version 1 of a Project note whose origin_id is the
// canonical note. Archived canonical notes are not copied, and later changes of the canon do not
// reach the copies.
func copyNotes(ctx context.Context, tx *sql.Tx, seriesID, projectID, now string) error {
	rows, err := tx.QueryContext(ctx, noteSelect+` WHERE n.series_id = ? AND n.archived_at IS NULL ORDER BY n.created_at, n.id`, seriesID)
	if err != nil {
		return fmt.Errorf("list canonical notes: %w", err)
	}
	var canon []domain.NoteView
	for rows.Next() {
		v, err := scanNoteView(rows)
		if err != nil {
			_ = rows.Close()
			return err
		}
		canon = append(canon, v)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, c := range canon {
		id := domain.NewID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO note (id, project_id, origin_id, category, pinned, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, projectID, c.ID, c.Category, boolInt(c.Pinned), now); err != nil {
			return fmt.Errorf("copy note: %w", err)
		}
		if err := insertNoteVersion(ctx, tx, id, 1, c.Version.Title, c.Version.Content, c.Version.Source, now); err != nil {
			return err
		}
	}
	return nil
}

// NewPromotion is a proposal to promote a note version into the World State.
type NewPromotion struct {
	NoteVersionID string
	// Proposed are the changes that can be applied, Conflicts those left out because the field is LOCKed.
	Proposed  domain.StateDiff
	Conflicts domain.StateDiff
}

// CreatePromotion stores the proposal for a version of a Project note together with its Approval
// card (chat_message with ref_type note_promotion; the caller sets Content), in one transaction. The
// card is non-blocking: the Project's state is not touched. It returns ErrNotFound for an unknown
// version or one of a Series note, and ErrConflict when the version was proposed before (a version
// is proposed once).
func (r *NoteRepository) CreatePromotion(ctx context.Context, p NewPromotion, card domain.ChatMessage) (domain.NotePromotion, domain.ChatMessage, error) {
	proposed, err := json.Marshal(nonNilDiff(p.Proposed))
	if err != nil {
		return domain.NotePromotion{}, domain.ChatMessage{}, err
	}
	conflicts, err := json.Marshal(nonNilDiff(p.Conflicts))
	if err != nil {
		return domain.NotePromotion{}, domain.ChatMessage{}, err
	}
	id := domain.NewID()
	now := formatTime(r.now())
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var projectID string
		err := tx.QueryRowContext(ctx,
			`SELECT n.project_id FROM note_version v JOIN note n ON n.id = v.note_id WHERE v.id = ? AND n.project_id IS NOT NULL`, p.NoteVersionID).Scan(&projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := exists(ctx, tx, `SELECT 1 FROM note_promotion WHERE note_version_id = ?`, p.NoteVersionID); err == nil {
			return ErrConflict
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO note_promotion (id, note_version_id, proposed_diff, locked_conflicts, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, p.NoteVersionID, string(proposed), string(conflicts), domain.PromotionPending, now); err != nil {
			return fmt.Errorf("insert note promotion: %w", err)
		}
		card.ID, card.ProjectID = domain.NewID(), projectID
		card.Role, card.Kind = domain.RoleAssistant, domain.KindApproval
		card.RefType, card.RefID, card.ApprovalStatus = domain.RefNotePromotion, id, domain.ApprovalPending
		return insertMessage(ctx, tx, card, now)
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return domain.NotePromotion{}, domain.ChatMessage{}, err
	}
	if err != nil {
		return domain.NotePromotion{}, domain.ChatMessage{}, fmt.Errorf("create note promotion: %w", err)
	}
	promo, err := r.Promotion(ctx, id)
	return promo, card, err
}

func nonNilDiff(d domain.StateDiff) domain.StateDiff {
	if d.Changes == nil {
		d.Changes = []domain.StateChange{}
	}
	return d
}

// Promotion returns a promotion (ErrNotFound for an unknown one).
func (r *NoteRepository) Promotion(ctx context.Context, id string) (domain.NotePromotion, error) {
	return readPromotion(ctx, r.db.Reader(), id)
}

func readPromotion(ctx context.Context, q queryer, id string) (domain.NotePromotion, error) {
	var (
		p                      domain.NotePromotion
		proposed, conflicts    string
		worldVersion, resolved sql.NullString
		createdAt              string
	)
	err := q.QueryRowContext(ctx,
		`SELECT p.id, p.note_version_id, v.note_id, COALESCE(n.project_id, ''), v.version_number, p.proposed_diff, p.locked_conflicts, p.status, p.world_state_version_id, p.created_at, p.resolved_at
		 FROM note_promotion p JOIN note_version v ON v.id = p.note_version_id JOIN note n ON n.id = v.note_id WHERE p.id = ?`, id).
		Scan(&p.ID, &p.NoteVersionID, &p.NoteID, &p.ProjectID, &p.VersionNumber, &proposed, &conflicts, &p.Status, &worldVersion, &createdAt, &resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("get note promotion: %w", err)
	}
	if err := json.Unmarshal([]byte(proposed), &p.Proposed); err != nil {
		return p, fmt.Errorf("decode proposed diff: %w", err)
	}
	if err := json.Unmarshal([]byte(conflicts), &p.Conflicts); err != nil {
		return p, fmt.Errorf("decode locked conflicts: %w", err)
	}
	p.WorldStateVersionID = worldVersion.String
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return p, err
	}
	if resolved.Valid {
		t, err := parseTime(resolved.String)
		if err != nil {
			return p, err
		}
		p.ResolvedAt = &t
	}
	return p, nil
}

// ResolvePromotion answers a pending promotion. Rejecting only marks it rejected. Approving writes
// its changes into the Project's trunk World State and appends a world_state_version, in one
// transaction (F-31, spec/spec.md §9.4): the LOCK is judged again on the data being written, and a
// change whose field is LOCKed now, whose entity is gone, or that the field already has, is left
// out (never an error). A promotion that leaves nothing to write appends no version. It returns
// ErrNotFound for an unknown promotion and ErrConflict for one that was answered already.
func (r *NoteRepository) ResolvePromotion(ctx context.Context, id string, approve bool) (domain.PromotionResult, error) {
	var res domain.PromotionResult
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		p, err := readPromotion(ctx, tx, id)
		if err != nil {
			return err
		}
		if p.Status != domain.PromotionPending {
			return ErrConflict
		}
		if !approve {
			_, err := tx.ExecContext(ctx, `UPDATE note_promotion SET status = ?, resolved_at = ? WHERE id = ?`, domain.PromotionRejected, now, id)
			return err
		}
		var versionID any
		if res, versionID, err = r.applyPromotion(ctx, tx, p); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE note_promotion SET status = ?, world_state_version_id = ?, resolved_at = ? WHERE id = ?`,
			domain.PromotionApplied, versionID, now, id)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return domain.PromotionResult{}, err
	}
	if err != nil {
		return domain.PromotionResult{}, fmt.Errorf("resolve note promotion: %w", err)
	}
	return res, nil
}

func (r *NoteRepository) applyPromotion(ctx context.Context, tx *sql.Tx, p domain.NotePromotion) (domain.PromotionResult, any, error) {
	res := domain.PromotionResult{Applied: domain.StateDiff{Changes: []domain.StateChange{}}, Skipped: domain.StateDiff{Changes: []domain.StateChange{}}}
	before, err := readWorld(ctx, tx, p.ProjectID, "")
	if err != nil {
		return res, nil, err
	}
	locks, err := readLocks(ctx, tx, p.ProjectID)
	if err != nil {
		return res, nil, err
	}
	for _, c := range p.Proposed.Changes {
		old, found := before.Current(c.Target, c.ID, c.Field)
		switch {
		case !domain.IsEditable(c.Target, c.Field) || domain.ValidateFieldValue(c.Target, c.Field, c.To) != nil:
			continue // not something a promotion may write
		case !found || locks.Locked(c.Target, c.ID, c.Field):
			res.Skipped.Changes = append(res.Skipped.Changes, c)
			continue
		case domain.SameValue(old, c.To):
			continue
		}
		var text string
		if err := json.Unmarshal(c.To, &text); err != nil {
			continue
		}
		if err := setField(ctx, tx, worldTables[c.Target], c.ID, c.Field, text); err != nil {
			return res, nil, err
		}
		c.From = old
		res.Applied.Changes = append(res.Applied.Changes, c)
	}
	if len(res.Applied.Changes) == 0 {
		return res, nil, nil
	}
	after, err := readWorld(ctx, tx, p.ProjectID, "")
	if err != nil {
		return res, nil, err
	}
	versionID, number, err := r.world.appendVersion(ctx, tx, p.ProjectID, "", after)
	if err != nil {
		return res, nil, err
	}
	res.Version = number
	return res, versionID, nil
}
