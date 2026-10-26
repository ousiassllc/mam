package comfywf_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// beforeComfyWorkflow is the migration before 20261025120000_comfy_workflow.sql.
const beforeComfyWorkflow = 20261025090000

func TestMigration_ComfyWorkflowUpAndDown(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	tables := func() int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'comfy_workflow%'`).Scan(&n))
		return n
	}

	_, err = provider.UpTo(ctx, beforeComfyWorkflow)
	require.NoError(t, err)
	assert.Zero(t, tables())
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, tables())

	_, err = db.ExecContext(ctx, `INSERT INTO comfy_workflow (id, name, kind, api_json) VALUES ('a', 'Flow', 'image', '{}')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO comfy_workflow (id, name, kind, api_json) VALUES ('b', 'flow', 'image', '{}')`)
	assert.Error(t, err, "names are unique ignoring case")
	_, err = db.ExecContext(ctx, `INSERT INTO comfy_workflow (id, name, kind, api_json) VALUES ('c', 'x', 'audio', '{}')`)
	assert.Error(t, err, "kind is image or video")
	_, err = db.ExecContext(ctx, `INSERT INTO comfy_workflow_version (workflow_id, version, api_json, assignment_json) VALUES ('a', 1, '{}', '{}')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM comfy_workflow WHERE id = 'a'`)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comfy_workflow_version`).Scan(&n))
	assert.Zero(t, n, "deleting a workflow deletes its versions")

	_, err = provider.DownTo(ctx, beforeComfyWorkflow)
	require.NoError(t, err)
	assert.Zero(t, tables())
}

func sample(id, name string, at time.Time) comfywf.Workflow {
	return comfywf.Workflow{
		ID: id, Name: name, Kind: "image", Endpoint: "bundled-comfyui", UIJSON: `{"nodes":[]}`,
		APIJSON: json.RawMessage(`{"1":{"class_type":"X","inputs":{}}}`), Assignment: json.RawMessage(`{"output_node":"1"}`),
		Version: 1, CreatedAt: at, UpdatedAt: at,
	}
}

func TestRepository_CreateGetUpdateAndVersions(t *testing.T) {
	ctx := context.Background()
	repo := comfywf.New(testdb.Open(t))
	at := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

	require.NoError(t, repo.Create(ctx, sample("w1", "Hero", at)))
	assert.ErrorIs(t, repo.Create(ctx, sample("w2", "hero", at)), repository.ErrConflict)

	got, err := repo.Get(ctx, "w1")
	require.NoError(t, err)
	assert.Equal(t, "Hero", got.Name)
	assert.Equal(t, `{"nodes":[]}`, got.UIJSON)
	assert.Equal(t, 1, got.Version)
	assert.True(t, at.Equal(got.CreatedAt))
	byName, err := repo.GetByName(ctx, "HERO")
	require.NoError(t, err)
	assert.Equal(t, "w1", byName.ID)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Empty(t, list[0].UIJSON, "the list leaves out the editor JSON")

	// An edit that bumps the version keeps the old snapshot.
	edited := got
	edited.Version, edited.UpdatedAt = 2, at.Add(time.Hour)
	edited.APIJSON = json.RawMessage(`{"1":{"class_type":"Y","inputs":{}}}`)
	require.NoError(t, repo.Update(ctx, edited))
	v1, err := repo.GetVersion(ctx, "w1", 1)
	require.NoError(t, err)
	assert.JSONEq(t, `{"1":{"class_type":"X","inputs":{}}}`, string(v1.APIJSON))
	v2, err := repo.GetVersion(ctx, "w1", 2)
	require.NoError(t, err)
	assert.JSONEq(t, `{"1":{"class_type":"Y","inputs":{}}}`, string(v2.APIJSON))
	all, err := repo.Versions(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// Editing the same version in place replaces its snapshot.
	inPlace := edited
	inPlace.APIJSON = json.RawMessage(`{"1":{"class_type":"Z","inputs":{}}}`)
	require.NoError(t, repo.Update(ctx, inPlace))
	v2, err = repo.GetVersion(ctx, "w1", 2)
	require.NoError(t, err)
	assert.JSONEq(t, `{"1":{"class_type":"Z","inputs":{}}}`, string(v2.APIJSON))

	// A rename does not add a version.
	renamed := inPlace
	renamed.Name = "Villain"
	require.NoError(t, repo.Update(ctx, renamed))
	all, _ = repo.Versions(ctx)
	assert.Len(t, all, 2)
	assert.ErrorIs(t, repo.Update(ctx, sample("missing", "z", at)), repository.ErrNotFound)

	require.NoError(t, repo.Delete(ctx, "w1"))
	assert.ErrorIs(t, repo.Delete(ctx, "w1"), repository.ErrNotFound)
	_, err = repo.GetVersion(ctx, "w1", 1)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
