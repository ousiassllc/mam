// Package worldtest builds the data the tests of the world, reference, branch, series, version and note
// tools need: a Project with a Scene and a Shot, a Character / Product / Location, and Library images.
package worldtest

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Project is what Seed made. The Character is "Mio" (id c1), the Product "Serum" (p1), the Location "Street" (l1).
type Project struct {
	ID, SceneID, ShotID string
}

var png = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

// Seed makes a Project (SeedProject) with a Character, a Product and a Location. Call it once per test
// database: the ids are fixed.
func Seed(t testing.TB, env *mcptest.Env) Project {
	t.Helper()
	p := SeedProject(t, env)
	Exec(t, env, `INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, p.ID)
	Exec(t, env, `INSERT INTO product (id, project_id, name) VALUES ('p1', ?, 'Serum')`, p.ID)
	Exec(t, env, `INSERT INTO location (id, project_id, name, time) VALUES ('l1', ?, 'Street', 'dusk')`, p.ID)
	return p
}

// SeedProject makes a Project with a Scene and a Shot.
func SeedProject(t testing.TB, env *mcptest.Env) Project {
	t.Helper()
	ctx := context.Background()
	p, err := env.Services.Projects.Create(ctx, service.CreateProjectInput{Content: "美容液の縦型 15 秒 CM を作りたい"})
	require.NoError(t, err)
	scene, err := env.Services.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: "Street"})
	require.NoError(t, err)
	shot, err := env.Services.Plans.CreateShot(ctx, scene.Scene.ID, service.ShotForm{Objective: "主人公が振り返る", DurationSeconds: "4"})
	require.NoError(t, err)
	return Project{ID: p.ID, SceneID: scene.Scene.ID, ShotID: shot.Shots[0].ID}
}

// Exec runs one statement against the test database.
func Exec(t testing.TB, env *mcptest.Env, q string, args ...any) {
	t.Helper()
	require.NoError(t, env.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// Count returns the first column of a single-row COUNT query.
func Count(t testing.TB, env *mcptest.Env, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, env.DB.Reader().QueryRow(q, args...).Scan(&n))
	return n
}

// Reference puts an image of the kind in the Reference Library of the Project and returns its entry.
func Reference(t testing.TB, env *mcptest.Env, projectID, kind, name string) domain.ReferenceItem {
	t.Helper()
	view, err := env.Services.Reference.Upload(context.Background(), projectID, service.UploadReferenceInput{
		File: service.Upload{Name: "a.png", Size: int64(len(png)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(png)), nil }},
		Kind: kind, Name: name,
	})
	require.NoError(t, err)
	return view.Items[0]
}
