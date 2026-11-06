package generation_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The reason a candidate run stops is the reason it had: a plan that cannot be made because the database failed is not a spent
// budget (#1062). The Take that exists is kept and recommended, and the notice says the next Take was not planned.
func TestOrchestrator_BalancedModeStopsWithTheTrueReasonWhenTheNextTakeCannotBePlanned(t *testing.T) {
	f := newFixture(t, options{withJudge: true})
	f.judge.verdicts = func(int) (domain.JudgeResult, error) {
		// The first Take is judged, and then the Shot's LOCKs can no longer be read: decide fails for the retry.
		require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `ALTER TABLE lock RENAME TO lock_gone`)
			return err
		}))
		return failing(), nil
	}

	require.NoError(t, f.candidates(t, ""))

	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take`), "no further Take is made")
	summary := f.chat.ofKind(domain.KindText)
	require.Len(t, summary, 1)
	assert.Contains(t, summary[0].Content, "次の生成を計画できなかった")
	assert.NotContains(t, summary[0].Content, "予算上限")
}
