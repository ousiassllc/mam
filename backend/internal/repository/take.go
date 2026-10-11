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

// GetMany returns the Takes with the given IDs, keyed by ID, in one query whatever their number. An
// ID that matches no Take is left out of the map.
func (r *TakeRepository) GetMany(ctx context.Context, takeIDs []string) (map[string]TakeDetail, error) {
	out := make(map[string]TakeDetail, len(takeIDs))
	if len(takeIDs) == 0 {
		return out, nil
	}
	ids, err := json.Marshal(takeIDs)
	if err != nil {
		return nil, fmt.Errorf("get takes: %w", err)
	}
	rows, err := r.db.Reader().QueryContext(ctx, takeInfoSelect+` WHERE t.id IN (SELECT value FROM json_each(?))`, string(ids))
	if err != nil {
		return nil, fmt.Errorf("get takes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		d, err := scanTakeDetail(rows)
		if err != nil {
			return nil, fmt.Errorf("get takes: %w", err)
		}
		out[d.ID] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get takes: %w", err)
	}
	return out, nil
}

// Select adopts the Take for its Shot as the user's choice: it becomes "selected" and the Shot's
// selected Take, and the Take that was selected before goes back to being a candidate. A Take that
// domain.Take.Selectable refuses (rejected, file archived, already selected) is left as it is
// (ErrConflict). It returns the Shot ID.
func (r *TakeRepository) Select(ctx context.Context, takeID string) (string, error) {
	var shotID string
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		take, err := loadActionTake(ctx, tx, takeID)
		if err != nil {
			return err
		}
		shotID = take.ShotID
		if !take.Selectable() {
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

// loadActionTake reads what domain.Take.Usable and Selectable decide on (the Shot, the status and
// the file of the Take), so that the repository refuses an action by the rule the API and the pages
// show it by. It returns ErrNotFound for a Take that does not exist.
func loadActionTake(ctx context.Context, tx *sql.Tx, takeID string) (domain.Take, error) {
	t := domain.Take{ID: takeID}
	err := tx.QueryRowContext(ctx,
		`SELECT pv.shot_id, t.status, COALESCE(t.file_path, '') FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE t.id = ?`,
		takeID).Scan(&t.ShotID, &t.Status, &t.FilePath)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Take{}, ErrNotFound
	}
	return t, err
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
// none afterwards. Its file and history are kept. A Take that is not usable (already rejected, or
// its file archived) is left as it is (ErrConflict). It returns the Shot ID.
func (r *TakeRepository) Reject(ctx context.Context, takeID string) (string, error) {
	var shotID string
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		take, err := loadActionTake(ctx, tx, takeID)
		if err != nil {
			return err
		}
		shotID = take.ShotID
		if !take.Usable() {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE take SET status = ? WHERE id = ?`, domain.TakeRejected, takeID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE shot SET selected_take_id = NULL WHERE id = ? AND selected_take_id = ?`, shotID, takeID)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
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
// Take that is acceptable also becomes the Shot's selected Take (AUTO); otherwise the AI Pick is
// only a recommendation (DIRECTOR). A Shot with a Take the user selected is left alone. The Takes
// are read and marked in one transaction, so Takes judged at the same time cannot leave a stale pick.
//
// accepted tells whether a Take meets the Retry Policy (the Minimum acceptance scores, the same rule
// the Repair loop applies): the Take AUTO adopts must be one, and when some Take is, the pick is the
// best of those. A Take that passed with scores under the minimum is only recommended, as the Repair
// loop treats it as not good enough. A Take the user rejected is out of the pool before accepted is
// asked (domain.PickAcceptedTake), so it cannot make the others fall short of the policy. A nil
// accepted counts every Take that passed as acceptable.
func (r *TakeRepository) PickBest(ctx context.Context, shotID string, adopt bool, accepted func(takeID string) bool) (AIPick, error) {
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
		id, passed, ok := domain.PickAcceptedTake(takes, accepted)
		if !ok {
			return nil
		}
		adoptable := passed && (accepted == nil || accepted(id))
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
		if adopt && adoptable {
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
	// Candidate leaves the Shot's selection alone: the Take is only added as a candidate (a Partial
	// Regeneration is the AI's proposal, not the user's choice).
	Candidate bool
}

// inheritLineage copies what the base Take was generated from (the World State version and the
// References, domain.TakeLineage) and how its model was chosen (the Multi-model routing decision)
// into the parameters of a composed Take, which is made from the base Take's prompt version and model
// and so from the same World State, References and routing. A base Take that predates a record has
// none to copy: the Generation Trace falls back as it does for that Take.
func inheritLineage(params json.RawMessage, base string) (json.RawMessage, error) {
	var from, to map[string]json.RawMessage
	if json.Unmarshal([]byte(base), &from) != nil {
		return params, nil
	}
	if err := json.Unmarshal(params, &to); err != nil {
		return nil, fmt.Errorf("decode composite parameters: %w", err)
	}
	for _, key := range []string{"world_state_version", "references", "routing"} {
		if v, ok := from[key]; ok {
			to[key] = v
		}
	}
	out, err := json.Marshal(to)
	if err != nil {
		return nil, fmt.Errorf("encode composite parameters: %w", err)
	}
	return out, nil
}

// CreateComposite stores the composed Take with its video asset. Unless it is a Candidate it is the
// user's choice: it becomes the Shot's selected Take with the status "selected". It returns the Take
// ID, or ErrNotFound when the base Take does not belong to the Shot.
func (r *TakeRepository) CreateComposite(ctx context.Context, nc NewComposite) (string, error) {
	takeID := domain.NewID()
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var promptVersionID, model, baseParams string
		err := tx.QueryRowContext(ctx,
			`SELECT t.prompt_version_id, t.model_name, COALESCE(t.model_parameters, '') FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
			 WHERE t.id = ? AND pv.shot_id = ?`, nc.BaseTakeID, nc.ShotID).Scan(&promptVersionID, &model, &baseParams)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		params, err := inheritLineage(nc.Parameters, baseParams)
		if err != nil {
			return err
		}
		now := formatTime(r.now())
		if err := insertAsset(ctx, tx, nc.Asset, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			takeID, promptVersionID, model, string(params), nc.Asset.FilePath, domain.TakeCandidate, now); err != nil {
			return fmt.Errorf("insert take: %w", err)
		}
		if nc.Candidate {
			return nil
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
