package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// TraceRepository reads the Generation Traceability of a Take (F-27, spec/spec.md §29.3): what
// generation stored with the Take, the Judge results and the Repair history.
type TraceRepository struct {
	db *DB
}

// NewTraceRepository returns a TraceRepository backed by db.
func NewTraceRepository(db *DB) *TraceRepository { return &TraceRepository{db: db} }

// TakeTrace returns everything that led to the Take, or ErrNotFound.
func (r *TraceRepository) TakeTrace(ctx context.Context, takeID string) (domain.TakeTrace, error) {
	q := r.db.Reader()
	var (
		tr                                       domain.TakeTrace
		takeAt, pvAt, snapshot, params, filePath string
		adapter                                  sql.NullString
		noteRefs                                 string
		assetID, mime                            sql.NullString
		size                                     sql.NullInt64
	)
	t, pv := &tr.Take, &tr.PromptVersion
	err := q.QueryRowContext(ctx,
		`SELECT t.id, pv.shot_id, t.prompt_version_id, t.model_name, t.model_parameters, COALESCE(t.file_path, ''), t.status, t.created_at,
		        sc.project_id, pv.version_number, pv.shot_spec_snapshot, pv.model_adapter_output, pv.note_refs, pv.created_at,
		        a.id, a.mime_type, a.size_bytes
		 FROM take t
		 JOIN prompt_version pv ON pv.id = t.prompt_version_id
		 JOIN shot sh ON sh.id = pv.shot_id
		 JOIN scene sc ON sc.id = sh.scene_id
		 LEFT JOIN asset a ON a.project_id = sc.project_id AND a.file_path = t.file_path
		 WHERE t.id = ?`, takeID).
		Scan(&t.ID, &pv.ShotID, &t.PromptVersionID, &t.ModelName, &params, &filePath, &t.Status, &takeAt,
			&tr.ProjectID, &pv.VersionNumber, &snapshot, &adapter, &noteRefs, &pvAt, &assetID, &mime, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TakeTrace{}, ErrNotFound
	}
	if err != nil {
		return domain.TakeTrace{}, fmt.Errorf("get take trace: %w", err)
	}
	t.ShotID, t.FilePath = pv.ShotID, filePath
	pv.ID = t.PromptVersionID
	pv.SpecSnapshot = json.RawMessage(snapshot)
	if adapter.Valid {
		pv.AdapterOutput = json.RawMessage(adapter.String)
	}
	if err := json.Unmarshal([]byte(noteRefs), &pv.NoteRefs); err != nil {
		return domain.TakeTrace{}, fmt.Errorf("decode note refs: %w", err)
	}
	tr.ModelParameters = json.RawMessage(params)
	if t.CreatedAt, err = parseTime(takeAt); err != nil {
		return domain.TakeTrace{}, err
	}
	if pv.CreatedAt, err = parseTime(pvAt); err != nil {
		return domain.TakeTrace{}, err
	}

	var lineage domain.TakeLineage
	_ = json.Unmarshal([]byte(params), &lineage) // parameters written before the lineage was recorded lack it
	tr.WorldStateVersion = lineage.WorldStateVersion
	tr.References = lineage.References
	if tr.References == nil {
		tr.References = domain.Shot{ShotSpec: pv.SpecSnapshot}.References()
	}
	var routed struct {
		Routing *domain.RoutingDecision `json:"routing"`
	}
	_ = json.Unmarshal([]byte(params), &routed) // parameters written before Multi-model routing lack it
	tr.Routing = routed.Routing
	if assetID.Valid {
		tr.Output = &domain.TraceOutput{AssetID: assetID.String, FilePath: filePath, MimeType: mime.String, SizeBytes: size.Int64}
	}

	if tr.Judgements, err = r.judgements(ctx, takeID); err != nil {
		return domain.TakeTrace{}, err
	}
	if tr.Repairs, err = r.repairs(ctx, repairSelect+
		` JOIN judge_result jr ON jr.id = ra.judge_result_id WHERE jr.take_id = ? ORDER BY ra.created_at, ra.rowid`, takeID); err != nil {
		return domain.TakeTrace{}, err
	}
	produced, err := r.repairs(ctx, repairSelect+` WHERE ra.resulting_take_id = ?`, takeID)
	if err != nil {
		return domain.TakeTrace{}, err
	}
	if len(produced) > 0 {
		tr.ProducedBy = &produced[0]
	}
	return tr, nil
}

func (r *TraceRepository) judgements(ctx context.Context, takeID string) ([]domain.JudgeResult, error) {
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

func (r *TraceRepository) repairs(ctx context.Context, query string, args ...any) ([]domain.RepairRecord, error) {
	rows, err := r.db.Reader().QueryContext(ctx, query, args...)
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
