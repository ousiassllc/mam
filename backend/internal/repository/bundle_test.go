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
	ctx := context.Background()
	// Only Export and Import run per case: a bundle is exported from a seeded database and imported
	// into an empty one, which must stay empty because every bundle is refused whole.
	source := openTestDB(t)
	seedDeepProject(t, source, "a")
	exporter := repository.NewBundleRepository(source)
	target := openTestDB(t)
	importer := repository.NewBundleRepository(target)
	for _, table := range tables {
		for _, id := range ids {
			data, err := exporter.Export(ctx, "a")
			require.NoError(t, err)
			setBundleField(t, data, table, "id", id)

			assert.ErrorIs(t, importer.Import(ctx, data), repository.ErrInvalidBundle, "%s.id %q", table, id)
			assert.Equal(t, map[string]int{"project": 0, "take": 0, "edit_timeline_version": 0},
				countRows(t, target, "project", "take", "edit_timeline_version"), "nothing is written")
		}
	}
}

// A row without an id would be inserted with a NULL primary key, which neither the id check nor the
// ownership check (fk NOT IN (parent ids)) sees: a missing or null id refuses the bundle in every table
// that has an id column, and a policy row without its project_id likewise.
func TestBundleRepository_ImportRejectsRowsWithoutAnID(t *testing.T) {
	ctx := context.Background()
	source := openTestDB(t)
	seedDeepProject(t, source, "a")
	require.NoError(t, exec(t, source, `INSERT INTO asset (id, project_id, type, file_path) VALUES ('a-as', 'a', 'image', 'x.png')`))
	require.NoError(t, exec(t, source, `INSERT INTO character (id, project_id, name, face_reference_asset_id) VALUES ('a-ch', 'a', 'Mio', 'a-as')`))
	require.NoError(t, exec(t, source, `INSERT INTO project_policy (project_id) VALUES ('a')`))
	exporter := repository.NewBundleRepository(source)
	target := openTestDB(t)
	importer := repository.NewBundleRepository(target)
	probe, err := exporter.Export(ctx, "a")
	require.NoError(t, err)
	checked := 0
	for _, tb := range probe.Tables {
		if len(tb.Rows) == 0 {
			continue
		}
		column := "id"
		if tb.Name == "project_policy" {
			column = "project_id"
		}
		for _, omit := range []string{"absent", "null"} {
			data, err := exporter.Export(ctx, "a")
			require.NoError(t, err)
			for _, t2 := range data.Tables {
				if t2.Name != tb.Name {
					continue
				}
				if omit == "absent" {
					delete(t2.Rows[0], column)
				} else {
					t2.Rows[0][column] = nil
				}
			}
			assert.ErrorIs(t, importer.Import(ctx, data), repository.ErrInvalidBundle, "%s without %s (%s)", tb.Name, column, omit)
			assert.Equal(t, map[string]int{"project": 0, "asset": 0}, countRows(t, target, "project", "asset"), "nothing is written")
		}
		checked++
	}
	assert.Greater(t, checked, 10, "the seeded project fills most tables")
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

func TestBundleRepository_ImportRejectsATakeCardThatLoadsFromAnotherHost(t *testing.T) {
	for name, content := range map[string]string{
		"video on another host":       `{"title":"T","video_url":"/\\evil.example/x.mp4"}`,
		"poster on another host":      `{"title":"T","video_url":"/takes/t/video","poster_url":"//evil.example/p.jpg"}`,
		"absolute video URL":          `{"title":"T","video_url":"https://evil.example/x.mp4"}`,
		"escaped backslash in poster": `{"title":"T","video_url":"/takes/t/video","poster_url":"/%5Cevil.example/p.jpg"}`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t)
			seedDeepProject(t, db, "a")
			r := repository.NewBundleRepository(db)
			data, err := r.Export(ctx, "a")
			require.NoError(t, err)
			require.NoError(t, exec(t, db, `DELETE FROM project WHERE id = 'a'`))
			for i := range data.Tables {
				if data.Tables[i].Name == "chat_message" {
					data.Tables[i].Rows = append(data.Tables[i].Rows, domain.BundleRow{
						"id": "m-take", "project_id": "a", "role": "assistant", "kind": "take", "content": content, "attachment_asset_ids": "[]",
					})
				}
			}
			assert.ErrorIs(t, r.Import(ctx, data), repository.ErrInvalidBundle)
			assert.Equal(t, 0, countRows(t, db, "chat_message")["chat_message"])
		})
	}
}

// The readers decode the JSON columns into typed values and parse the timestamps: a bundle row that
// does not fit would import and then break every read of the Project (the list, the Workspace).
func TestBundleRepository_ImportRejectsColumnsTheReadersCannotDecode(t *testing.T) {
	cases := []struct {
		name, table, column string
		value               any
	}{
		{"custom checkpoints that are not a list", "project", "custom_checkpoints", `{"script":true}`},
		{"a checkpoint nobody asks at", "project", "custom_checkpoints", `["script","nowhere"]`},
		{"a project created at no time", "project", "created_at", "yesterday"},
		{"a project created at a time of another format", "project", "created_at", "2026-01-02 03:04:05"},
		{"a take created at no time", "take", "created_at", ""},
		{"a timestamp that is a number", "take", "created_at", float64(1)},
		{"issues that are not a list", "judge_result", "issues", `{"a":1}`},
		{"a world state that is not an object", "world_state_version", "state", `[]`},
		{"a timeline that is not an object", "edit_timeline_version", "timeline", `"x"`},
		{"findings that are not a list", "editor_judge_result", "findings", `7`},
		{"a shot spec that is not an object", "shot", "shot_spec", `[]`},
		{"a shot order that is text", "shot", "order_index", "x"},
		{"a shot order that is a fraction", "shot", "order_index", json.Number("1.5")},
		{"a scene order that is text", "scene", "order_index", "a"},
		{"a shot duration that is text", "shot", "duration_seconds", "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			src := openTestDB(t)
			seedDeepProject(t, src, "a")
			data, err := repository.NewBundleRepository(src).Export(ctx, "a")
			require.NoError(t, err)
			setBundleField(t, data, tc.table, tc.column, tc.value)

			dst := openTestDB(t)
			assert.ErrorIs(t, repository.NewBundleRepository(dst).Import(ctx, data), repository.ErrInvalidBundle)
			assert.Equal(t, 0, countRows(t, dst, "project")["project"], "nothing is written")
		})
	}
}

func TestBundleRepository_ImportKeepsCustomCheckpoints(t *testing.T) {
	ctx := context.Background()
	src := openTestDB(t)
	seedDeepProject(t, src, "a")
	require.NoError(t, exec(t, src, `UPDATE project SET custom_checkpoints = '["script","final"]'`))
	data, err := repository.NewBundleRepository(src).Export(ctx, "a")
	require.NoError(t, err)

	dst := openTestDB(t)
	require.NoError(t, repository.NewBundleRepository(dst).Import(ctx, data))
	var cps string
	require.NoError(t, dst.Reader().QueryRow(`SELECT custom_checkpoints FROM project WHERE id = 'a'`).Scan(&cps))
	assert.JSONEq(t, `["script","final"]`, cps)
}
