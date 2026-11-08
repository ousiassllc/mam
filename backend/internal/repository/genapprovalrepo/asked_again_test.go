package genapprovalrepo_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// msg is a row of chat_message: id, role, and the card it replies to ("" for none).
type msg struct{ id, role, replyTo string }

// TestAskedAgainAfter: the decline is the user's FIRST reply to the card, or the card itself, and never before the card. A message that is not a reply to
// the card lifts the decline when it was written after that; a later reply to the answered card (an ordinary message) does not move the baseline.
func TestAskedAgainAfter(t *testing.T) {
	for name, tc := range map[string]struct {
		rows []msg // in the order they were written (rowid)
		want bool
	}{
		"the card alone, nobody wrote":                   {[]msg{{"card", "assistant", ""}}, false},
		"a message after the card, nobody replied":       {[]msg{{"card", "assistant", ""}, {"free", "user", ""}}, true},
		"a message before the decline lifts nothing":     {[]msg{{"card", "assistant", ""}, {"wait", "user", ""}, {"decline", "user", "card"}}, false},
		"a message after the decline lifts it":           {[]msg{{"card", "assistant", ""}, {"decline", "user", "card"}, {"free", "user", ""}}, true},
		"a message of the assistant lifts nothing":       {[]msg{{"card", "assistant", ""}, {"decline", "user", "card"}, {"note", "assistant", ""}}, false},
		"a late reply does not hide the message between": {[]msg{{"card", "assistant", ""}, {"decline", "user", "card"}, {"free", "user", ""}, {"late", "user", "card"}}, true},
		"a late reply alone lifts nothing":               {[]msg{{"card", "assistant", ""}, {"decline", "user", "card"}, {"late", "user", "card"}}, false},
		"a reply that came before the card is no baseline above the card": {
			[]msg{{"decline", "user", "card"}, {"card", "assistant", ""}, {"free", "user", ""}}, true},
		"a message before the card and its early reply lifts nothing": {
			[]msg{{"free", "user", ""}, {"decline", "user", "card"}, {"card", "assistant", ""}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := testdb.Open(t)
			require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p', 'auto', 'full_auto', 'product_cm', 'balanced')`); err != nil {
					return err
				}
				for _, m := range tc.rows {
					kind, status := "text", any(nil)
					if m.id == "card" {
						kind, status = "approval", "changes_requested"
					}
					var replyTo any
					if m.replyTo != "" {
						replyTo = m.replyTo
					}
					if _, err := tx.ExecContext(ctx, `INSERT INTO chat_message (id, project_id, role, kind, reply_to_message_id, approval_status) VALUES (?, 'p', ?, ?, ?, ?)`,
						m.id, m.role, kind, replyTo, status); err != nil {
						return err
					}
				}
				return nil
			}))

			got, err := genapprovalrepo.New(db).AskedAgainAfter(ctx, "p", "card")

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
