package gentestrepo

import (
	"context"
	"database/sql"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

const promptColumns = `id, prompt, negative_prompt, use_count, created_at, last_used_at`

// Prompts returns the prompt history, the most recently used first.
func (r *Repository) Prompts(ctx context.Context) ([]Prompt, error) {
	return queryAll(ctx, r, scanPrompt, `SELECT `+promptColumns+` FROM gentest_prompt ORDER BY last_used_at DESC, id DESC`)
}

// GetPrompt returns one prompt, or repository.ErrNotFound.
func (r *Repository) GetPrompt(ctx context.Context, id string) (Prompt, error) {
	p, err := scanPrompt(r.db.Reader().QueryRowContext(ctx, `SELECT `+promptColumns+` FROM gentest_prompt WHERE id = ?`, id))
	return p, notFound(err)
}

// DeletePrompt removes a prompt from the history. The results that used it keep their own copy. An unknown prompt is
// repository.ErrNotFound.
func (r *Repository) DeletePrompt(ctx context.Context, id string) error {
	return r.exec(ctx, `DELETE FROM gentest_prompt WHERE id = ?`, id)
}

// AddImport records that a result was imported into a Project. The result must exist (repository.ErrNotFound).
func (r *Repository) AddImport(ctx context.Context, im Import) (Import, error) {
	if im.ID == "" {
		im.ID = domain.NewID()
	}
	if im.CreatedAt.IsZero() {
		im.CreatedAt = time.Now().UTC()
	}
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM gentest_result WHERE id = ?`, im.ResultID).Scan(&one); err != nil {
			return notFound(err)
		}
		var item any
		if im.ReferenceItemID != "" {
			item = im.ReferenceItemID
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO gentest_import (id, result_id, project_id, asset_id, reference_item_id, target, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			im.ID, im.ResultID, im.ProjectID, im.AssetID, item, string(orJSON(im.Target, "{}")), ts(im.CreatedAt))
		return err
	})
	return im, err
}

// Imports returns where the result was imported to, newest first.
func (r *Repository) Imports(ctx context.Context, resultID string) ([]Import, error) {
	return queryAll(ctx, r, scanImport,
		`SELECT id, result_id, project_id, asset_id, reference_item_id, target, created_at FROM gentest_import WHERE result_id = ? ORDER BY created_at DESC, id DESC`, resultID)
}
