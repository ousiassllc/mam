package testdb_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// queryPlan returns the EXPLAIN QUERY PLAN detail lines joined by newlines.
func queryPlan(t *testing.T, db *repository.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.Reader().Query(`EXPLAIN QUERY PLAN `+query, args...)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		plan = append(plan, detail)
	}
	require.NoError(t, rows.Err())
	return strings.Join(plan, "\n")
}

// ChatRepository.ListByRef runs on every job state change and Judge save.
func TestSchema_ChatMessageRefLookupUsesIndex(t *testing.T) {
	plan := queryPlan(t, testdb.Open(t), `SELECT id FROM chat_message WHERE ref_type = ? AND ref_id = ? ORDER BY id`, "job", "j1")
	assert.Contains(t, plan, "idx_chat_message_ref")
	assert.NotContains(t, plan, "SCAN chat_message")
}

// ledgerBudget sums by shot_id; deleting a shot / take searches by shot_id / take_id (ON DELETE SET NULL).
func TestSchema_CostLedgerShotAndTakeLookupsUseIndexes(t *testing.T) {
	db := testdb.Open(t)
	shotPlan := queryPlan(t, db, `SELECT SUM(COALESCE(actual_cost, estimated_cost)) FROM cost_ledger_entry WHERE shot_id = ?`, "s1")
	assert.Contains(t, shotPlan, "idx_cost_ledger_shot")
	assert.NotContains(t, shotPlan, "SCAN cost_ledger_entry")

	takePlan := queryPlan(t, db, `SELECT id FROM cost_ledger_entry WHERE take_id = ?`, "t1")
	assert.Contains(t, takePlan, "idx_cost_ledger_take")
	assert.NotContains(t, takePlan, "SCAN cost_ledger_entry")
}
