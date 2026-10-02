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

// RepairRepository persists the Repair / Retake Loop: the repair actions (repair_action), which link
// a Judge result to the Take that repairs it, and the shadow runs of Jev (repair_shadow). A repair
// never changes an existing Take: it only adds a new one. All writes go through DB.Write.
type RepairRepository struct {
	db  *DB
	now func() time.Time
}

// NewRepairRepository returns a RepairRepository backed by db.
func NewRepairRepository(db *DB) *RepairRepository {
	return &RepairRepository{db: db, now: time.Now}
}

// CreateAction records the decision to repair the Take that judgeResultID judged. The resulting Take
// is set later (SetResultingTake), once it exists.
func (r *RepairRepository) CreateAction(ctx context.Context, judgeResultID string, action domain.RepairAction, ins domain.RepairInstructions) (domain.RepairRecord, error) {
	raw, err := json.Marshal(ins)
	if err != nil {
		return domain.RepairRecord{}, fmt.Errorf("encode repair instructions: %w", err)
	}
	rec := domain.RepairRecord{ID: domain.NewID(), JudgeResultID: judgeResultID, Action: action, Instructions: ins, CreatedAt: r.now().UTC()}
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO repair_action (id, judge_result_id, action_type, instructions, created_at) VALUES (?, ?, ?, ?, ?)`,
			rec.ID, judgeResultID, action, string(raw), formatTime(rec.CreatedAt))
		return err
	})
	if err != nil {
		return domain.RepairRecord{}, fmt.Errorf("create repair action: %w", err)
	}
	return rec, nil
}

// SetResultingTake records the new Take the repair action made.
func (r *RepairRepository) SetResultingTake(ctx context.Context, actionID, takeID string) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE repair_action SET resulting_take_id = ? WHERE id = ?`, takeID, actionID)
		if err != nil {
			return fmt.Errorf("set resulting take: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

const repairSelect = `SELECT ra.id, ra.judge_result_id, ra.action_type, ra.instructions, COALESCE(ra.resulting_take_id, ''), ra.created_at FROM repair_action ra`

func scanRepair(row rowScanner) (domain.RepairRecord, error) {
	var (
		rec       domain.RepairRecord
		ins, made string
	)
	if err := row.Scan(&rec.ID, &rec.JudgeResultID, &rec.Action, &ins, &rec.ResultingTakeID, &made); err != nil {
		return domain.RepairRecord{}, err
	}
	if err := json.Unmarshal([]byte(ins), &rec.Instructions); err != nil {
		return domain.RepairRecord{}, fmt.Errorf("decode repair instructions: %w", err)
	}
	t, err := parseTime(made)
	if err != nil {
		return domain.RepairRecord{}, err
	}
	rec.CreatedAt = t
	return rec, nil
}

// ProducedBy returns the repair action that made the Take, or ErrNotFound when the Take was not
// made by a repair (it is a first generation).
func (r *RepairRepository) ProducedBy(ctx context.Context, takeID string) (domain.RepairRecord, error) {
	rec, err := scanRepair(r.db.Reader().QueryRowContext(ctx, repairSelect+` WHERE ra.resulting_take_id = ?`, takeID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RepairRecord{}, ErrNotFound
	}
	if err != nil {
		return domain.RepairRecord{}, fmt.Errorf("get repair action: %w", err)
	}
	return rec, nil
}

// ShotActions returns the repair actions of the Takes of the Shot, oldest first: its Repair history.
func (r *RepairRepository) ShotActions(ctx context.Context, shotID string) ([]domain.RepairRecord, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		repairSelect+` JOIN judge_result jr ON jr.id = ra.judge_result_id
		 JOIN take t ON t.id = jr.take_id
		 JOIN prompt_version pv ON pv.id = t.prompt_version_id
		 WHERE pv.shot_id = ? ORDER BY ra.created_at, ra.rowid`, shotID)
	if err != nil {
		return nil, fmt.Errorf("list repair actions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.RepairRecord
	for rows.Next() {
		rec, err := scanRepair(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repair action: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// CandidateTakeCount is how many Takes Prompt Candidate Generation made for the Shot, rejected ones
// included: they were paid for and count against the Retry Policy (domain.CandidateRetries).
func (r *RepairRepository) CandidateTakeCount(ctx context.Context, shotID string) (int, error) {
	var n int
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
		 WHERE pv.shot_id = ? AND COALESCE(json_extract(t.model_parameters, '$.candidate_strategy'), '') <> ''`, shotID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count candidate takes: %w", err)
	}
	return n, nil
}

// TakeVerdict is a Take of a Shot with its newest Judge result.
type TakeVerdict struct {
	Take domain.Take
	// Result is nil when the Take was never judged.
	Result *domain.JudgeResult
	// Pending is true while a judge job of the Take is queued or running.
	Pending bool
}

// ShotVerdicts returns the Takes of the Shot that are still in play (not rejected), oldest first,
// with their newest Judge result.
func (r *RepairRepository) ShotVerdicts(ctx context.Context, shotID string) ([]TakeVerdict, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT t.id, pv.shot_id, t.prompt_version_id, t.model_name, COALESCE(t.file_path, ''), t.status, t.created_at
		 FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
		 WHERE pv.shot_id = ? AND t.status <> ? ORDER BY t.created_at, t.rowid`, shotID, domain.TakeRejected)
	if err != nil {
		return nil, fmt.Errorf("list shot takes: %w", err)
	}
	var out []TakeVerdict
	for rows.Next() {
		var (
			t       domain.Take
			created string
		)
		if err := rows.Scan(&t.ID, &t.ShotID, &t.PromptVersionID, &t.ModelName, &t.FilePath, &t.Status, &created); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan take: %w", err)
		}
		if t.CreatedAt, err = parseTime(created); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, TakeVerdict{Take: t})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for i := range out {
		id := out[i].Take.ID
		res, err := scanJudge(r.db.Reader().QueryRowContext(ctx, judgeSelect+` WHERE take_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, id))
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return nil, fmt.Errorf("get judge result: %w", err)
		default:
			out[i].Result = &res
		}
		if err := r.db.Reader().QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM job WHERE kind = ? AND status IN (?, ?) AND json_extract(payload, '$.take_id') = ?)`,
			domain.JobJudge, domain.JobQueued, domain.JobRunning, id).Scan(&out[i].Pending); err != nil {
			return nil, fmt.Errorf("count judge jobs: %w", err)
		}
	}
	return out, nil
}

// SaveShadow stores one shadow run of Jev.
func (r *RepairRepository) SaveShadow(ctx context.Context, s domain.RepairShadow) error {
	probs, err := json.Marshal(s.Probabilities)
	if err != nil {
		return fmt.Errorf("encode probabilities: %w", err)
	}
	if s.Probabilities == nil {
		probs = []byte("{}")
	}
	var confidence any
	if s.Confidence != nil {
		confidence = *s.Confidence
	}
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO repair_shadow (id, repair_action_id, model, jev_choice, confidence, probabilities, decided_action, agreed,
			                            latency_ms, input_tokens, output_tokens, cost_usd, error, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			domain.NewID(), s.RepairActionID, s.Model, s.JevChoice, confidence, string(probs), s.DecidedAction, s.Agreed,
			s.LatencyMillis, s.InputTokens, s.OutputTokens, s.CostUSD, s.Error, formatTime(r.now()))
		if err != nil {
			return fmt.Errorf("save repair shadow: %w", err)
		}
		return nil
	})
}

// ShadowReport summarises every shadow run: the agreement with the Repair Agent, the confidence
// distribution, the latency and the cost.
func (r *RepairRepository) ShadowReport(ctx context.Context) (domain.ShadowReport, error) {
	rows, err := r.db.Reader().QueryContext(ctx, `SELECT confidence, agreed, latency_ms, cost_usd, error FROM repair_shadow`)
	if err != nil {
		return domain.ShadowReport{}, fmt.Errorf("list repair shadows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var (
		rep                       domain.ShadowReport
		confidenceSum, latencySum float64
		succeeded                 int
	)
	for rows.Next() {
		var (
			confidence sql.NullFloat64
			agreed     bool
			latency    int64
			cost       float64
			failure    string
		)
		if err := rows.Scan(&confidence, &agreed, &latency, &cost, &failure); err != nil {
			return domain.ShadowReport{}, fmt.Errorf("scan repair shadow: %w", err)
		}
		rep.Runs++
		rep.TotalCostUSD += cost
		if failure != "" || !confidence.Valid {
			rep.Errors++
			continue
		}
		succeeded++
		latencySum += float64(latency) // a failed call records no latency: it would pull the mean down
		confidenceSum += confidence.Float64
		bucket := min(int(confidence.Float64*domain.ConfidenceBuckets), domain.ConfidenceBuckets-1)
		bucket = max(bucket, 0)
		rep.Confidence[bucket]++
		if agreed {
			rep.Agreed++
			rep.ConfidenceAgreed[bucket]++
		}
	}
	if err := rows.Err(); err != nil {
		return domain.ShadowReport{}, err
	}
	if succeeded > 0 {
		rep.AgreementRate = float64(rep.Agreed) / float64(succeeded)
		rep.MeanConfidence = confidenceSum / float64(succeeded)
		rep.MeanLatencyMillis = latencySum / float64(succeeded)
	}
	return rep, nil
}
