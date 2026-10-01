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

func TestBundleRepository_AnEpisodeIsExportedDetachedFromItsSeries(t *testing.T) {
	ctx := context.Background()
	src := openTestDB(t)
	seedDeepProject(t, src, "a")
	for _, q := range []string{
		`INSERT INTO series (id, name) VALUES ('s', 'Show')`,
		`INSERT INTO series_version (id, series_id, version_number, state) VALUES ('sv', 's', 1, '{}')`,
		`INSERT INTO character (id, series_id, name) VALUES ('canon', 's', 'Mio')`,
		`INSERT INTO character (id, project_id, origin_id, name) VALUES ('copy', 'a', 'canon', 'Mio')`,
		`UPDATE project SET series_id = 's', episode_number = 2, inherited_series_version_id = 'sv', inherited_world_state_version_id = 'a-w' WHERE id = 'a'`,
	} {
		require.NoError(t, exec(t, src, q), q)
	}
	data, err := repository.NewBundleRepository(src).Export(ctx, "a")
	require.NoError(t, err)

	tables := map[string][]domain.BundleRow{}
	for _, tb := range data.Tables {
		tables[tb.Name] = tb.Rows
	}
	require.Len(t, tables["project"], 1)
	for _, col := range []string{"series_id", "episode_number", "inherited_series_version_id", "inherited_world_state_version_id"} {
		assert.Nil(t, tables["project"][0][col], col)
	}
	require.Len(t, tables["character"], 1, "the Series' canonical Character is not part of the Project")
	assert.Nil(t, tables["character"][0]["origin_id"])

	dst := openTestDB(t)
	require.NoError(t, repository.NewBundleRepository(dst).Import(ctx, data), "no foreign key points at the Series that is not there")
}

// setBundleField changes one field of the first row of a table in an exported bundle.
func setBundleField(t *testing.T, data domain.BundleData, table, column string, value any) {
	t.Helper()
	for _, tb := range data.Tables {
		if tb.Name == table {
			require.NotEmpty(t, tb.Rows, table)
			tb.Rows[0][column] = value
			return
		}
	}
	require.Failf(t, "no such table in the bundle", table)
}

func TestBundleRepository_ImportRejectsRowsThatReachOutsideTheBundle(t *testing.T) {
	cases := []struct {
		name, table, column string
		value               any
	}{
		{"a shot of another project's scene", "shot", "scene_id", "b-sc"},
		{"a prompt version of another project's shot", "prompt_version", "shot_id", "b-sh"},
		{"a take of another project's prompt version", "take", "prompt_version_id", "b-pv"},
		{"a judge result of another project's take", "judge_result", "take_id", "b-t"},
		{"a selected take of another project", "shot", "selected_take_id", "b-t"},
		{"an existing series", "project", "series_id", "s"},
		{"an existing series version", "project", "inherited_series_version_id", "sv"},
		{"an existing character as origin", "character", "origin_id", "b-ch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t)
			seedDeepProject(t, db, "a")
			seedDeepProject(t, db, "b")
			for _, q := range []string{
				`INSERT INTO series (id, name) VALUES ('s', 'Show')`,
				`INSERT INTO series_version (id, series_id, version_number, state) VALUES ('sv', 's', 1, '{}')`,
				`INSERT INTO character (id, project_id, name) VALUES ('b-ch', 'b', 'Mio')`,
				`INSERT INTO character (id, project_id, name) VALUES ('a-ch', 'a', 'Mio')`,
			} {
				require.NoError(t, exec(t, db, q), q)
			}
			r := repository.NewBundleRepository(db)
			data, err := r.Export(ctx, "a")
			require.NoError(t, err)
			require.NoError(t, exec(t, db, `DELETE FROM project WHERE id = 'a'`)) // foreign keys cascade
			setBundleField(t, data, tc.table, tc.column, tc.value)

			before := countRows(t, db, "shot", "project", "character")
			err = r.Import(ctx, data)
			assert.ErrorIs(t, err, repository.ErrInvalidBundle)
			assert.Equal(t, before, countRows(t, db, "shot", "project", "character"), "nothing is written")
		})
	}
}

func countRows(t *testing.T, db *repository.DB, tables ...string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range tables {
		var n int
		require.NoError(t, db.Reader().QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
		out[table] = n
	}
	return out
}

func TestBundleRepository_ImportRejectsProjectIDsThatCollideWithServerDirectories(t *testing.T) {
	for _, id := range []string{".previews", ".staging", `a"b`, "a?b"} {
		ctx := context.Background()
		db := openTestDB(t)
		seedDeepProject(t, db, "a")
		r := repository.NewBundleRepository(db)
		data, err := r.Export(ctx, "a")
		require.NoError(t, err)
		data.ProjectID = id
		assert.ErrorIs(t, r.Import(ctx, data), repository.ErrInvalidBundle, id)
	}
}

// The ID of a row is joined into file paths (audio directory, previews), so a bundle with an ID
// that is not [A-Za-z0-9_-]{1,64} is refused whole.
func TestBundleRepository_ImportRejectsRowIDsThatCouldEscapeAsFilePaths(t *testing.T) {
	tables := []string{"take", "edit_timeline_version", "editor_judge_result", "state_diff", "repair_shadow", "lock", "scene"}
	ids := []string{
		"x/../y", "x/../../y", "x/../../../../y", "..", ".", ".hidden", `a\b`, `..\..\x`, "a/b", "a%2Fb", "a\x00b", "a\nb",
		"", strings.Repeat("x", 65),
	}
	for _, table := range tables {
		for _, id := range ids {
			ctx := context.Background()
			db := openTestDB(t)
			seedDeepProject(t, db, "a")
			r := repository.NewBundleRepository(db)
			data, err := r.Export(ctx, "a")
			require.NoError(t, err)
			require.NoError(t, exec(t, db, `DELETE FROM project WHERE id = 'a'`)) // foreign keys cascade
			setBundleField(t, data, table, "id", id)

			assert.ErrorIs(t, r.Import(ctx, data), repository.ErrInvalidBundle, "%s.id %q", table, id)
			assert.Equal(t, map[string]int{"project": 0, "take": 0, "edit_timeline_version": 0},
				countRows(t, db, "project", "take", "edit_timeline_version"), "nothing is written")
		}
	}
}

func TestBundleRepository_ImportKeepsGeneratedStyleRowIDs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedDeepProject(t, db, "a")
	r := repository.NewBundleRepository(db)
	data, err := r.Export(ctx, "a")
	require.NoError(t, err)
	require.NoError(t, exec(t, db, `DELETE FROM project WHERE id = 'a'`))
	setBundleField(t, data, "scene", "id", "0b9f1a52-3c44-4e7e-9d6a-1c2d3e4f5a6b")
	setBundleField(t, data, "shot", "scene_id", "0b9f1a52-3c44-4e7e-9d6a-1c2d3e4f5a6b")

	require.NoError(t, r.Import(ctx, data))
	assert.Equal(t, 1, countRows(t, db, "scene")["scene"])
}
