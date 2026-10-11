package bundle_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/bundle"
)

func TestBundleRepository_ImportRejectsProjectIDsThatCollideWithServerDirectories(t *testing.T) {
	for _, id := range []string{".previews", ".staging", `a"b`, "a?b"} {
		ctx := context.Background()
		db := openTestDB(t)
		seedDeepProject(t, db, "a")
		r := bundle.New(db)
		data, err := r.Export(ctx, "a")
		require.NoError(t, err)
		data.ProjectID = id
		assert.ErrorIs(t, r.Import(ctx, data), bundle.ErrInvalid, id)
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
	exporter := bundle.New(source)
	target := openTestDB(t)
	importer := bundle.New(target)
	for _, table := range tables {
		for _, id := range ids {
			data, err := exporter.Export(ctx, "a")
			require.NoError(t, err)
			setBundleField(t, data, table, "id", id)

			assert.ErrorIs(t, importer.Import(ctx, data), bundle.ErrInvalid, "%s.id %q", table, id)
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
	exporter := bundle.New(source)
	target := openTestDB(t)
	importer := bundle.New(target)
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
			assert.ErrorIs(t, importer.Import(ctx, data), bundle.ErrInvalid, "%s without %s (%s)", tb.Name, column, omit)
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
	r := bundle.New(db)
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
			r := bundle.New(db)
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
			assert.ErrorIs(t, r.Import(ctx, data), bundle.ErrInvalid)
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
			data, err := bundle.New(src).Export(ctx, "a")
			require.NoError(t, err)
			setBundleField(t, data, tc.table, tc.column, tc.value)

			dst := openTestDB(t)
			assert.ErrorIs(t, bundle.New(dst).Import(ctx, data), bundle.ErrInvalid)
			assert.Equal(t, 0, countRows(t, dst, "project")["project"], "nothing is written")
		})
	}
}

func TestBundleRepository_ImportKeepsCustomCheckpoints(t *testing.T) {
	ctx := context.Background()
	src := openTestDB(t)
	seedDeepProject(t, src, "a")
	require.NoError(t, exec(t, src, `UPDATE project SET custom_checkpoints = '["script","final"]'`))
	data, err := bundle.New(src).Export(ctx, "a")
	require.NoError(t, err)

	dst := openTestDB(t)
	require.NoError(t, bundle.New(dst).Import(ctx, data))
	var cps string
	require.NoError(t, dst.Reader().QueryRow(`SELECT custom_checkpoints FROM project WHERE id = 'a'`).Scan(&cps))
	assert.JSONEq(t, `["script","final"]`, cps)
}
