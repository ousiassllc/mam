package genstage_test

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

// reimport exports the Project and imports it again as the other instance gets it: the bundle carries no generation_approval record.
func (r *rig) reimport(projectID string) {
	r.t.Helper()
	data, err := bundle.New(r.db).Export(context.Background(), projectID)
	require.NoError(r.t, err)
	require.NoError(r.t, r.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM project WHERE id = ?`, projectID)
		return err
	}))
	require.NoError(r.t, bundle.New(r.db).Import(context.Background(), data))
}

// Choosing AUTO is the consent to the paid generation on the instance where it was chosen (#1115): a bundle must not carry it. An imported AUTO Project
// is DIRECTOR, so nothing starts for it, whatever legacy card it holds, until the user chooses AUTO here; then the legacy rules apply as before.
func TestImportedAutoProject_StartsNothingUntilTheUserChoosesAutoHere(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		card      *domain.ApprovalStatus
		wantPacks int // Reference Packs started once AUTO is chosen here
	}{
		{"no legacy card", nil, 3},
		{"approved card without a record", new(domain.ApprovalApproved), 3},
		{"pending card without a record", new(domain.ApprovalPending), 3},
		{"declined card without a record", new(domain.ApprovalChangesRequested), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			id := r.planned(domain.ConfirmFullAuto)
			if c.card != nil {
				r.legacyCard(id, *c.card)
			}

			r.reimport(id)
			r.handle(id)

			assert.Equal(t, domain.ModeDirector, r.project1(id).Mode, "the AUTO of the bundle is not this instance's consent")
			assert.Empty(t, r.gen.started(), "nothing is started for an imported Project")
			assert.Empty(t, r.chat.superseded, "a pending card is left as it is")
			assert.Zero(t, r.announcements())

			r.setMode(id, domain.ModeAuto) // the user's choice on this instance
			r.handle(id)
			assert.Equal(t, c.wantPacks, r.gen.count(genapprovalrepo.StageReferencePack))
		})
	}
}
