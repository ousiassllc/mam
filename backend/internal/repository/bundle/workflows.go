package bundle

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// querier is what a *sql.DB and a *sql.Tx have in common.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// MissingWorkflows are the saved ComfyUI workflows (comfy:<id>@v<N>) that the Takes and the Video Model locks of the
// Project name and that this instance does not have (the workflow, or that version of it, is not saved). A bundle
// does not carry workflows (they are global), so an import warns about them; the Project is usable either way, and a
// Shot whose Video Model lock names a workflow that is missing is routed as if the model were AUTO (an unknown
// model in a lock is ignored, generation.FixedModel) until the workflow is imported or another model is chosen.
func (r *Repository) MissingWorkflows(ctx context.Context, projectID string) ([]string, error) {
	return missingWorkflows(ctx, r.db.Reader(), projectID)
}

func missingWorkflows(ctx context.Context, q querier, projectID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT model FROM (
			SELECT t.model_name AS model FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
				JOIN shot sh ON sh.id = pv.shot_id JOIN scene sc ON sc.id = sh.scene_id WHERE sc.project_id = ?1
			UNION ALL
			SELECT json_extract(value, '$') FROM lock WHERE project_id = ?1 AND parameter_name = 'video_model' AND value IS NOT NULL
		) WHERE model LIKE 'comfy:%' ORDER BY model`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list workflows of the project: %w", err)
	}
	var named []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		named = append(named, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	var missing []string
	for _, id := range named {
		wf, version, ok := modelreg.ParseWorkflowID(id)
		if !ok {
			continue
		}
		var n int
		if err := q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM comfy_workflow_version WHERE workflow_id = ? AND version = ?`, wf, version).Scan(&n); err != nil {
			return nil, fmt.Errorf("check workflow %s: %w", id, err)
		}
		if n == 0 {
			missing = append(missing, id)
		}
	}
	slices.Sort(missing)
	return missing, nil
}

// warnMissingWorkflows leaves a note in the chat of the imported Project when it names workflows this instance lacks.
func warnMissingWorkflows(ctx context.Context, tx *sql.Tx, projectID string) error {
	missing, err := missingWorkflows(ctx, tx, projectID)
	if err != nil || len(missing) == 0 {
		return err
	}
	note := "このプロジェクトが使っている ComfyUI ワークフローが、この環境にありません: " + strings.Join(missing, "、") +
		"。プロジェクトのバンドルにワークフローは含まれません。ノード画面の「ワークフロー」で取り込むか、Shot の Video Model を選び直してください。"
	_, err = tx.ExecContext(ctx,
		`INSERT INTO chat_message (id, project_id, role, kind, content) VALUES (?, ?, 'assistant', 'text', ?)`,
		domain.NewID(), projectID, note)
	return err
}
