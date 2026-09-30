package repository

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ModelRecords returns, for every video model that has made a judged Take in the Project, what the
// AI Video Judge found: the number of Takes, the mean of each score axis and the share of Takes
// whose latest Judge result passed (Multi-model routing, spec/architecture/model-adapters.md §12).
// A Take counts once, by its latest Judge result and the scores that follow it.
func (r *GenerationRepository) ModelRecords(ctx context.Context, projectID string) (map[domain.ModelName]domain.ModelRecord, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT t.model_name, COUNT(*),
		        AVG(json_extract(t.scores, '$.product')), AVG(json_extract(t.scores, '$.acting')),
		        AVG(json_extract(t.scores, '$.camera')), AVG(json_extract(t.scores, '$.continuity')),
		        AVG(CASE WHEN jr.result = 'pass' THEN 1.0 ELSE 0.0 END)
		 FROM take t
		 JOIN prompt_version pv ON pv.id = t.prompt_version_id
		 JOIN shot sh ON sh.id = pv.shot_id
		 JOIN scene sc ON sc.id = sh.scene_id
		 JOIN judge_result jr ON jr.id = (SELECT id FROM judge_result WHERE take_id = t.id ORDER BY created_at DESC, rowid DESC LIMIT 1)
		 WHERE sc.project_id = ? AND t.scores IS NOT NULL
		 GROUP BY t.model_name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("model records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[domain.ModelName]domain.ModelRecord{}
	for rows.Next() {
		var rec domain.ModelRecord
		if err := rows.Scan(&rec.Model, &rec.Samples, &rec.Scores.Product, &rec.Scores.Acting, &rec.Scores.Camera, &rec.Scores.Continuity, &rec.PassRate); err != nil {
			return nil, fmt.Errorf("scan model record: %w", err)
		}
		out[rec.Model] = rec
	}
	return out, rows.Err()
}
