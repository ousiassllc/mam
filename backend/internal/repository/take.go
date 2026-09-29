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

// TakeRepository persists the choice among the Takes of a Shot (F-15, spec/spec.md §14): the AI
// Pick, the Take the user selects, the Takes that are rejected and the Take composed from segments
// of others. All writes go through DB.Write.
type TakeRepository struct {
	db  *DB
	now func() time.Time
}

// NewTakeRepository returns a TakeRepository backed by db.
func NewTakeRepository(db *DB) *TakeRepository { return &TakeRepository{db: db, now: time.Now} }

// TakeDetail is a Take with the Project it belongs to.
type TakeDetail struct {
	domain.TakeInfo
	ProjectID string
}

const takeInfoSelect = `
SELECT t.id, pv.shot_id, t.prompt_version_id, t.model_name, COALESCE(t.file_path, ''), t.status, t.scores, t.model_parameters, t.created_at,
       sc.project_id,
       COALESCE((SELECT a.id FROM asset a WHERE a.project_id = sc.project_id AND a.file_path = t.file_path LIMIT 1), ''),
       COALESCE((SELECT j.result FROM judge_result j WHERE j.take_id = t.id ORDER BY j.created_at DESC, j.rowid DESC LIMIT 1), ''),
       COALESCE(sh.selected_take_id, '') = t.id
FROM take t
JOIN prompt_version pv ON pv.id = t.prompt_version_id
JOIN shot sh ON sh.id = pv.shot_id
JOIN scene sc ON sc.id = sh.scene_id`

// takeParameters is what the comparison reads of take.model_parameters.
type takeParameters struct {
	Resolution      string  `json:"resolution"`
	DurationSeconds float64 `json:"duration_seconds"`
}

func scanTakeDetail(row rowScanner) (TakeDetail, error) {
	var (
		d              TakeDetail
		scores         sql.NullString
		params, create string
		verdict        string
	)
	t := &d.TakeInfo
	if err := row.Scan(&t.ID, &t.ShotID, &t.PromptVersionID, &t.ModelName, &t.FilePath, &t.Status, &scores, &params, &create,
		&d.ProjectID, &t.AssetID, &verdict, &t.Adopted); err != nil {
		return TakeDetail{}, err
	}
	t.Verdict = domain.JudgeVerdict(verdict)
	if scores.Valid {
		var s domain.TakeScores
		if err := json.Unmarshal([]byte(scores.String), &s); err != nil {
			return TakeDetail{}, fmt.Errorf("decode take scores: %w", err)
		}
		t.Scores = &s
	}
	var p takeParameters
	_ = json.Unmarshal([]byte(params), &p) // parameters of another shape only lose the resolution and the length
	t.Resolution, t.DurationSeconds = p.Resolution, p.DurationSeconds
	var err error
	if t.CreatedAt, err = parseTime(create); err != nil {
		return TakeDetail{}, err
	}
	return d, nil
}

// ShotTakes returns the Takes of the Shot, oldest first.
func (r *TakeRepository) ShotTakes(ctx context.Context, shotID string) ([]domain.TakeInfo, error) {
	return shotTakes(ctx, r.db.Reader(), shotID)
}

func shotTakes(ctx context.Context, q queryer, shotID string) ([]domain.TakeInfo, error) {
	rows, err := q.QueryContext(ctx, takeInfoSelect+` WHERE pv.shot_id = ? ORDER BY t.created_at, t.id`, shotID)
	if err != nil {
		return nil, fmt.Errorf("list shot takes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.TakeInfo
	for rows.Next() {
		d, err := scanTakeDetail(rows)
		if err != nil {
			return nil, fmt.Errorf("scan take: %w", err)
		}
		out = append(out, d.TakeInfo)
	}
	return out, rows.Err()
}

// Get returns the Take or ErrNotFound.
func (r *TakeRepository) Get(ctx context.Context, takeID string) (TakeDetail, error) {
	d, err := scanTakeDetail(r.db.Reader().QueryRowContext(ctx, takeInfoSelect+` WHERE t.id = ?`, takeID))
	if errors.Is(err, sql.ErrNoRows) {
		return TakeDetail{}, ErrNotFound
	}
	if err != nil {
		return TakeDetail{}, fmt.Errorf("get take: %w", err)
	}
	return d, nil
}

// Select adopts the Take for its Shot as the user's choice: it becomes "selected" and the Shot's
// selected Take, and the Take that was selected before goes back to being a candidate. A Take whose
// file was archived cannot be selected (ErrConflict). It returns the Shot ID.
func (r *TakeRepository) Select(ctx context.Context, takeID string) (string, error) {
	var shotID string
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var path string
		err := tx.QueryRowContext(ctx,
			`SELECT pv.shot_id, COALESCE(t.file_path, '') FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE t.id = ?`,
			takeID).Scan(&shotID, &path)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if path == "" {
			return ErrConflict
		}
		return adoptTake(ctx, tx, shotID, takeID, domain.TakeSelected)
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("select take: %w", err)
	}
	return shotID, nil
}

// adoptTake makes the Take the Shot's selected Take with the status, and puts the Take that was
// selected before back to being a candidate.
func adoptTake(ctx context.Context, tx *sql.Tx, shotID, takeID string, status domain.TakeStatus) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE take SET status = ? WHERE status = ? AND id <> ? AND prompt_version_id IN (SELECT id FROM prompt_version WHERE shot_id = ?)`,
		domain.TakeCandidate, domain.TakeSelected, takeID, shotID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE take SET status = ? WHERE id = ?`, status, takeID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE shot SET selected_take_id = ? WHERE id = ?`, takeID, shotID)
	return err
}

// Reject excludes the Take (status "rejected"). When it was the Shot's selected Take the Shot has
// none afterwards. Its file and history are kept. It returns the Shot ID.
func (r *TakeRepository) Reject(ctx context.Context, takeID string) (string, error) {
	var shotID string
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx,
			`SELECT pv.shot_id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE t.id = ?`, takeID).Scan(&shotID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE take SET status = ? WHERE id = ?`, domain.TakeRejected, takeID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE shot SET selected_take_id = NULL WHERE id = ? AND selected_take_id = ?`, shotID, takeID)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("reject take: %w", err)
	}
	return shotID, nil
}

// AIPick is what the AI Pick of a Shot decided.
type AIPick struct {
	// TakeID is the picked Take ("" when nothing was picked).
	TakeID string
	// Passed tells whether the picked Take passed the Judge.
	Passed bool
	// Changed is true when the Take became the AI Pick, or the Shot's selected Take, just now.
	Changed bool
	// Adopted is true when the Take is the Shot's selected Take afterwards.
	Adopted bool
}

// PickBest marks the best judged Take of the Shot as its AI Pick (domain.PickTake). With adopt, a
// Take that passed the Judge also becomes the Shot's selected Take (AUTO); otherwise the AI Pick is
// only a recommendation (DIRECTOR). A Shot with a Take the user selected is left alone. The Takes
// are read and marked in one transaction, so Takes judged at the same time cannot leave a stale pick.
func (r *TakeRepository) PickBest(ctx context.Context, shotID string, adopt bool) (AIPick, error) {
	var pick AIPick
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		pick = AIPick{}
		takes, err := shotTakes(ctx, tx, shotID)
		if err != nil {
			return err
		}
		for _, t := range takes {
			if t.Status == domain.TakeSelected {
				return nil // the user's choice stands
			}
		}
		id, passed, ok := domain.PickTake(takes)
		if !ok {
			return nil
		}
		pick = AIPick{TakeID: id, Passed: passed}
		adopted := ""
		for _, t := range takes {
			if t.Adopted {
				adopted = t.ID
			}
			if t.ID == id && t.Status != domain.TakeAIPick {
				pick.Changed = true
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE take SET status = ? WHERE status = ? AND id <> ? AND prompt_version_id IN (SELECT id FROM prompt_version WHERE shot_id = ?)`,
			domain.TakeCandidate, domain.TakeAIPick, id, shotID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE take SET status = ? WHERE id = ?`, domain.TakeAIPick, id); err != nil {
			return err
		}
		if adopt && passed {
			if adopted != id {
				pick.Changed = true
			}
			_, err = tx.ExecContext(ctx, `UPDATE shot SET selected_take_id = ? WHERE id = ?`, id, shotID)
			pick.Adopted = true
			return err
		}
		pick.Adopted = adopted == id
		return nil
	})
	if err != nil {
		return AIPick{}, fmt.Errorf("pick best take: %w", err)
	}
	return pick, nil
}

// NewComposite is a Take made from segments of other Takes of the Shot (Partial Take Use).
type NewComposite struct {
	ProjectID string
	ShotID    string
	// BaseTakeID is the first segment's Take: the composed Take takes its prompt version and model.
	BaseTakeID string
	Parameters json.RawMessage
	Asset      domain.Asset // Type video; ID and FilePath are set by the caller
}

// CreateComposite stores the composed Take with its video asset. It is the user's choice: it becomes
// the Shot's selected Take with the status "selected". It returns the Take ID, or ErrNotFound when
// the base Take does not belong to the Shot.
func (r *TakeRepository) CreateComposite(ctx context.Context, nc NewComposite) (string, error) {
	takeID := domain.NewID()
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var promptVersionID, model string
		err := tx.QueryRowContext(ctx,
			`SELECT t.prompt_version_id, t.model_name FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
			 WHERE t.id = ? AND pv.shot_id = ?`, nc.BaseTakeID, nc.ShotID).Scan(&promptVersionID, &model)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		now := formatTime(r.now())
		if err := insertAsset(ctx, tx, nc.Asset, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			takeID, promptVersionID, model, string(nc.Parameters), nc.Asset.FilePath, domain.TakeCandidate, now); err != nil {
			return fmt.Errorf("insert take: %w", err)
		}
		return adoptTake(ctx, tx, nc.ShotID, takeID, domain.TakeSelected)
	})
	if errors.Is(err, ErrNotFound) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("create composite take: %w", err)
	}
	return takeID, nil
}
