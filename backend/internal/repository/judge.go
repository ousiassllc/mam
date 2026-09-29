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

// JudgeRepository persists the AI Video Judge's results (judge_result) and reads what the Judge
// needs about a Take. Every run is kept: judge_result.take_id is not unique. All writes go through DB.Write.
type JudgeRepository struct {
	db  *DB
	now func() time.Time
}

// NewJudgeRepository returns a JudgeRepository backed by db.
func NewJudgeRepository(db *DB) *JudgeRepository {
	return &JudgeRepository{db: db, now: time.Now}
}

// TakeRef is a Take with the Project and Shot it belongs to and the Shot Spec it was generated from.
type TakeRef struct {
	Take      domain.Take
	ProjectID string
	// SpecSnapshot is prompt_version.shot_spec_snapshot: the Shot Spec (and Reference Pack) as they
	// were when the Take was generated, which is what the Take is judged against.
	SpecSnapshot json.RawMessage
}

// GetTake returns the Take or ErrNotFound.
func (r *JudgeRepository) GetTake(ctx context.Context, takeID string) (TakeRef, error) {
	var (
		ref     TakeRef
		created string
		spec    string
	)
	t := &ref.Take
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT t.id, pv.shot_id, t.prompt_version_id, t.model_name, COALESCE(t.file_path, ''), t.status, t.created_at,
		        sc.project_id, pv.shot_spec_snapshot
		 FROM take t
		 JOIN prompt_version pv ON pv.id = t.prompt_version_id
		 JOIN shot sh ON sh.id = pv.shot_id
		 JOIN scene sc ON sc.id = sh.scene_id
		 WHERE t.id = ?`, takeID).
		Scan(&t.ID, &t.ShotID, &t.PromptVersionID, &t.ModelName, &t.FilePath, &t.Status, &created, &ref.ProjectID, &spec)
	if errors.Is(err, sql.ErrNoRows) {
		return TakeRef{}, ErrNotFound
	}
	if err != nil {
		return TakeRef{}, fmt.Errorf("get take: %w", err)
	}
	if t.CreatedAt, err = parseTime(created); err != nil {
		return TakeRef{}, err
	}
	ref.SpecSnapshot = json.RawMessage(spec)
	return ref, nil
}

// Save stores a Judge result for its Take as a new row and returns it with ID and CreatedAt set.
// The Take's scores (take.scores, the four scores of the Take comparison) follow its latest result.
func (r *JudgeRepository) Save(ctx context.Context, res domain.JudgeResult) (domain.JudgeResult, error) {
	issues, metrics, preserve, repair, err := res.EncodeJSON()
	scores, scoresErr := json.Marshal(domain.ScoreTake(res.Issues))
	if err == nil {
		err = scoresErr
	}
	if err != nil {
		return domain.JudgeResult{}, fmt.Errorf("encode judge result: %w", err)
	}
	var repairColumn any // NULL when the Judge recommended nothing
	if res.RepairStrategy != nil {
		repairColumn = string(repair)
	}
	res.ID = domain.NewID()
	res.CreatedAt = r.now().UTC()
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO judge_result (id, take_id, result, overall_confidence, issues, metrics, preserve, repair_strategy, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			res.ID, res.TakeID, res.Result, res.OverallConfidence, string(issues), string(metrics), string(preserve), repairColumn,
			formatTime(res.CreatedAt)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE take SET scores = ? WHERE id = ?`, string(scores), res.TakeID)
		return err
	})
	if err != nil {
		return domain.JudgeResult{}, fmt.Errorf("save judge result: %w", err)
	}
	return res, nil
}

const judgeSelect = `SELECT id, take_id, result, COALESCE(overall_confidence, 0), issues, metrics, preserve, repair_strategy, created_at FROM judge_result`

// Latest returns the newest Judge result of the Take, or ErrNotFound when it was never judged.
func (r *JudgeRepository) Latest(ctx context.Context, takeID string) (domain.JudgeResult, error) {
	res, err := scanJudge(r.db.Reader().QueryRowContext(ctx, judgeSelect+` WHERE take_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, takeID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.JudgeResult{}, ErrNotFound
	}
	if err != nil {
		return domain.JudgeResult{}, fmt.Errorf("get judge result: %w", err)
	}
	return res, nil
}

// History returns every Judge result of the Take, newest first.
func (r *JudgeRepository) History(ctx context.Context, takeID string) ([]domain.JudgeResult, error) {
	rows, err := r.db.Reader().QueryContext(ctx, judgeSelect+` WHERE take_id = ? ORDER BY created_at DESC, rowid DESC`, takeID)
	if err != nil {
		return nil, fmt.Errorf("list judge results: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.JudgeResult
	for rows.Next() {
		res, err := scanJudge(rows)
		if err != nil {
			return nil, fmt.Errorf("scan judge result: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

func scanJudge(row rowScanner) (domain.JudgeResult, error) {
	var (
		res                                  domain.JudgeResult
		issues, metrics, preserve, createdAt string
		repair                               sql.NullString
	)
	if err := row.Scan(&res.ID, &res.TakeID, &res.Result, &res.OverallConfidence, &issues, &metrics, &preserve, &repair, &createdAt); err != nil {
		return domain.JudgeResult{}, err
	}
	if err := json.Unmarshal([]byte(issues), &res.Issues); err != nil {
		return domain.JudgeResult{}, fmt.Errorf("decode judge issues: %w", err)
	}
	if err := json.Unmarshal([]byte(metrics), &res.Metrics); err != nil {
		return domain.JudgeResult{}, fmt.Errorf("decode judge metrics: %w", err)
	}
	// preserve is a list; the schema's default '{}' (a row nobody wrote through Save) reads as empty.
	if err := json.Unmarshal([]byte(preserve), &res.Preserve); err != nil {
		res.Preserve = nil
	}
	if repair.Valid && repair.String != "null" {
		var rs domain.RepairStrategy
		if err := json.Unmarshal([]byte(repair.String), &rs); err != nil {
			return domain.JudgeResult{}, fmt.Errorf("decode repair strategy: %w", err)
		}
		res.RepairStrategy = &rs
	}
	res.Issues = nonNilSlice(res.Issues)
	res.Preserve = nonNilSlice(res.Preserve)
	t, err := parseTime(createdAt)
	if err != nil {
		return domain.JudgeResult{}, err
	}
	res.CreatedAt = t
	return res, nil
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Locks returns the LOCKed fields of the Project (lock.mode = 'lock').
func (r *JudgeRepository) Locks(ctx context.Context, projectID string) (domain.LockSet, error) {
	return readLocks(ctx, r.db.Reader(), projectID)
}

// JudgePending reports whether a judge job of the Take is queued or running.
func (r *JudgeRepository) JudgePending(ctx context.Context, takeID string) (bool, error) {
	var n int
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM job WHERE kind = ? AND status IN (?, ?) AND json_extract(payload, '$.take_id') = ?`,
		domain.JobJudge, domain.JobQueued, domain.JobRunning, takeID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count judge jobs: %w", err)
	}
	return n > 0, nil
}

// BeginJudging moves the Shot from PLANNING to JUDGING. A Shot that is being generated (or
// repaired, edited) keeps its state: judging is the lowest-priority activity of a Shot.
func (r *JudgeRepository) BeginJudging(ctx context.Context, shotID string) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE shot SET state = ? WHERE id = ? AND state = ?`, domain.ShotJudging, shotID, domain.ShotPlanning)
		return err
	})
}

// EndJudging moves the Shot from JUDGING back to PLANNING unless another judge job of the Shot
// (other than exceptJobID, the one that is ending) is still queued or running.
func (r *JudgeRepository) EndJudging(ctx context.Context, shotID, exceptJobID string) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE shot SET state = ? WHERE id = ? AND state = ?
			   AND NOT EXISTS (SELECT 1 FROM job WHERE kind = ? AND status IN (?, ?) AND id <> ? AND json_extract(payload, '$.shot_id') = ?)`,
			domain.ShotPlanning, shotID, domain.ShotJudging, domain.JobJudge, domain.JobQueued, domain.JobRunning, exceptJobID, shotID)
		return err
	})
}
