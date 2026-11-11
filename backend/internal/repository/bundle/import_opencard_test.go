package bundle_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/bundle"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// An exported open confirmation card (ref_type "operation") comes back as a plain question: the app is not waiting for it, so it neither stops the
// Orchestrator's paid generation (OperationCardOpen) nor stands for a key card the chat would read every reply as a key to.
func TestBundleRepository_ImportedOpenOperationCardIsNoOpenConfirmationCard(t *testing.T) {
	for _, kind := range []string{"question", "escalation"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t)
			seedDeepProject(t, db, "a")
			r := bundle.New(db)
			data, err := r.Export(ctx, "a")
			require.NoError(t, err)
			require.NoError(t, exec(t, db, `DELETE FROM project WHERE id = 'a'`))
			for i := range data.Tables {
				if data.Tables[i].Name == "chat_message" {
					data.Tables[i].Rows = append(data.Tables[i].Rows, domain.BundleRow{
						"id": "m-open", "project_id": "a", "role": "assistant", "kind": kind, "ref_type": domain.RefOperation, "ref_id": "set_budget",
						"content": `{"text":"予算を $20 にしますか？","options":["はい","やめる"],"option_actions":[{"type":"set_budget"},null]}`, "attachment_asset_ids": "[]",
					})
				}
			}

			require.NoError(t, r.Import(ctx, data))

			var refType, refID sql.NullString
			require.NoError(t, db.Reader().QueryRow(`SELECT ref_type, ref_id FROM chat_message WHERE id = 'm-open'`).Scan(&refType, &refID))
			assert.False(t, refType.Valid, "the card no longer says it confirms an operation")
			assert.False(t, refID.Valid)
			open, err := genapprovalrepo.New(db).OperationCardOpen(ctx, "a", "set_budget")
			require.NoError(t, err)
			assert.False(t, open, "no confirmation card of the budget waits for an answer")
			latest, err := genapprovalrepo.New(db).LatestOperationCards(ctx, "a")
			require.NoError(t, err)
			assert.Empty(t, latest)
		})
	}
}

// The cards that are not operation confirmations keep what says what they ask (the Brief question, Continue).
func TestBundleRepository_ImportKeepsTheReferenceOfAnotherQuestionCard(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedDeepProject(t, db, "a")
	r := bundle.New(db)
	data, err := r.Export(ctx, "a")
	require.NoError(t, err)
	require.NoError(t, exec(t, db, `DELETE FROM project WHERE id = 'a'`))
	for i := range data.Tables {
		if data.Tables[i].Name == "chat_message" {
			data.Tables[i].Rows = append(data.Tables[i].Rows, domain.BundleRow{
				"id": "m-brief", "project_id": "a", "role": "assistant", "kind": "question", "ref_type": domain.RefBriefField, "ref_id": "tone",
				"content": `{"text":"トーンは？"}`, "attachment_asset_ids": "[]",
			})
		}
	}
	require.NoError(t, r.Import(ctx, data))

	var refType, refID string
	require.NoError(t, db.Reader().QueryRow(`SELECT ref_type, ref_id FROM chat_message WHERE id = 'm-brief'`).Scan(&refType, &refID))
	assert.Equal(t, domain.RefBriefField, refType)
	assert.Equal(t, "tone", refID)
}
