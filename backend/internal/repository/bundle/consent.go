package bundle

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ImportedAutoNotice is what the chat of an imported Project says when its AUTO mode was not carried over.
const ImportedAutoNotice = "このプロジェクトは別の環境から読み込んだもので、MODE が AUTO でした。AUTO を選ぶことは有料の生成への同意なので、" +
	"この環境には引き継がず、DIRECTOR にしました。この環境で AUTO で進めるなら、MODE で AUTO を選び直してください（それがこの環境での同意になります）。"

// dropImportedAuto sets an imported Project to DIRECTOR when the bundle carried AUTO, and leaves a note in its chat. Choosing AUTO is the consent
// to the paid generation of the pipeline on the instance where it was chosen (spec/spec.md §4.3): a bundle from someone else must not carry that
// consent to this instance, where the first look of the Orchestrator would start generating. The user chooses AUTO again here, and that is the
// consent. Every other column is as the bundle has it.
func dropImportedAuto(ctx context.Context, tx *sql.Tx, projectID string) error {
	res, err := tx.ExecContext(ctx, `UPDATE project SET mode = ? WHERE id = ? AND mode = ?`, domain.ModeDirector, projectID, domain.ModeAuto)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO chat_message (id, project_id, role, kind, content) VALUES (?, ?, 'assistant', 'text', ?)`,
		domain.NewID(), projectID, ImportedAutoNotice)
	return err
}
