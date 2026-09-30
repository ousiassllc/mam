package repository_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// seedDeepProject inserts a Project with rows in every table that hangs off a Project through
// another table (Take → Judge result → Repair action → shadow, Shot → State diff, timeline →
// Editor Judge result), every ID is 'P' or starts with "'P-", which the helper replaces with p.
func seedDeepProject(t *testing.T, db *repository.DB, p string) {
	t.Helper()
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('P', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, order_index) VALUES ('P-sc', 'P', 0)`,
			`INSERT INTO shot (id, scene_id, order_index) VALUES ('P-sh', 'P-sc', 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('P-pv', 'P-sh', 1, '{}')`,
			`INSERT INTO take (id, prompt_version_id, model_name) VALUES ('P-t', 'P-pv', 'kling-3.0')`,
			`UPDATE shot SET selected_take_id = 'P-t' WHERE id = 'P-sh'`,
			`INSERT INTO judge_result (id, take_id, result) VALUES ('P-j', 'P-t', 'fail')`,
			`INSERT INTO repair_action (id, judge_result_id, action_type) VALUES ('P-ra', 'P-j', 'full_retake')`,
			`INSERT INTO repair_shadow (id, repair_action_id, model, decided_action) VALUES ('P-rs', 'P-ra', 'jev', 'full_retake')`,
			`INSERT INTO world_state_version (id, project_id, version_number, state) VALUES ('P-w', 'P', 1, '{}')`,
			`INSERT INTO state_diff (id, shot_id, take_id, diff, world_state_version_id) VALUES ('P-sd', 'P-sh', 'P-t', '{}', 'P-w')`,
			`INSERT INTO edit_timeline_version (id, project_id, version_number, timeline) VALUES ('P-e', 'P', 1, '{}')`,
			`INSERT INTO editor_judge_result (id, edit_timeline_version_id, result) VALUES ('P-ej', 'P-e', 'pass')`,
			`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('P-l', 'P', 'shot', 'P-sh', 'prompt', 'lock')`,
		} {
			q = strings.ReplaceAll(q, "'P", "'"+p)
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

func rowCounts(t *testing.T, data domain.BundleData) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tb := range data.Tables {
		out[tb.Name] = len(tb.Rows)
	}
	return out
}

func TestBundleRepository_ExportTakesOnlyTheProjectAndImportRestoresEveryRow(t *testing.T) {
	ctx := context.Background()
	src := openTestDB(t)
	seedDeepProject(t, src, "a")
	seedDeepProject(t, src, "b")
	r := repository.NewBundleRepository(src)

	data, err := r.Export(ctx, "a")
	require.NoError(t, err)

	counts := rowCounts(t, data)
	for _, table := range []string{"project", "scene", "shot", "prompt_version", "take", "judge_result", "repair_action", "repair_shadow",
		"world_state_version", "state_diff", "edit_timeline_version", "editor_judge_result", "lock"} {
		assert.Equal(t, 1, counts[table], "%s: only the rows of Project a", table)
	}

	// Through JSON, as a bundle file carries it.
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	var decoded domain.BundleData
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	require.NoError(t, dec.Decode(&decoded))

	dst := openTestDB(t)
	require.NoError(t, repository.NewBundleRepository(dst).Import(ctx, decoded))
	again, err := repository.NewBundleRepository(dst).Export(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, data, again, "what was imported exports identically")

	var selected string
	require.NoError(t, dst.Reader().QueryRow(`SELECT selected_take_id FROM shot WHERE id = 'a-sh'`).Scan(&selected))
	assert.Equal(t, "a-t", selected, "the cyclic shot ↔ take reference survives")

	assert.ErrorIs(t, repository.NewBundleRepository(dst).Import(ctx, decoded), repository.ErrConflict)
	_, err = r.Export(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestBundleRepository_ImportOfInFlightShotsDoesNotLeaveThemRunning(t *testing.T) {
	ctx := context.Background()
	src := openTestDB(t)
	seedDeepProject(t, src, "a")
	require.NoError(t, exec(t, src, `UPDATE shot SET state = 'GENERATING'`))
	data, err := repository.NewBundleRepository(src).Export(ctx, "a")
	require.NoError(t, err)

	dst := openTestDB(t)
	require.NoError(t, repository.NewBundleRepository(dst).Import(ctx, data))

	var state string
	require.NoError(t, dst.Reader().QueryRow(`SELECT state FROM shot WHERE id = 'a-sh'`).Scan(&state))
	assert.NotEqual(t, "GENERATING", state, "no job runs in the imported Project")
}
