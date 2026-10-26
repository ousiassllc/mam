package shotplan_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotplan"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
)

func newFixture(t *testing.T) *shottest.Fixture {
	t.Helper()
	return shottest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { shotplan.Register(r, env.Services) }, shottest.Options{})
}

func TestCreateSceneAndShot_AppearInTheHierarchyInOrder(t *testing.T) {
	f := newFixture(t)

	var scene shotplan.CreateSceneOutput
	f.CallOK("create_scene", map[string]any{"project_id": f.ProjectID, "title": "Rooftop", "location": "屋上", "time": "夕方"}, &scene)
	assert.Equal(t, "Rooftop", scene.Scene.Title)

	var shot shotplan.CreateShotOutput
	f.CallOK("create_shot", map[string]any{"scene_id": scene.Scene.ID, "objective": "夕日を見上げる", "duration_seconds": 3.5}, &shot)
	assert.Equal(t, "夕日を見上げる", shot.Shot.Objective)
	assert.InDelta(t, 3.5, shot.Shot.DurationSeconds, 0.001)
	assert.Equal(t, scene.Scene.ID, shot.Shot.SceneID)

	var h shotplan.HierarchyOutput
	f.CallOK("get_scene_hierarchy", map[string]any{"project_id": f.ProjectID}, &h)
	require.Len(t, h.Scenes, 2)
	assert.Equal(t, f.SceneID, h.Scene.ID, "the first Scene is selected when scene_id is left out")
	assert.Equal(t, 1, h.Scenes[0].ShotCount)

	f.CallOK("get_scene_hierarchy", map[string]any{"project_id": f.ProjectID, "scene_id": scene.Scene.ID}, &h)
	require.Len(t, h.Shots, 1)
	assert.Equal(t, shot.Shot.ID, h.Shots[0].ID)
	assert.NotNil(t, h.Shots[0].Takes, "an empty list, not null")
}

func TestCreateSceneAndShot_RejectTheInputTheScreenRejects(t *testing.T) {
	f := newFixture(t)
	assert.Contains(t, f.CallError("create_scene", map[string]any{"project_id": f.ProjectID, "title": "  "}), "validation")
	assert.Contains(t, f.CallError("create_scene", map[string]any{"project_id": f.ProjectID, "title": strings.Repeat("あ", 500)}), "validation")
	assert.Contains(t, f.CallError("create_scene", map[string]any{"project_id": "missing", "title": "x"}), "not_found")

	assert.Contains(t, f.CallError("create_shot", map[string]any{"scene_id": f.SceneID, "objective": "", "duration_seconds": 3}), "validation")
	assert.Contains(t, f.CallError("create_shot", map[string]any{"scene_id": f.SceneID, "objective": "x", "duration_seconds": 0}), "validation")
	assert.Contains(t, f.CallError("create_shot", map[string]any{"scene_id": f.SceneID, "objective": "x", "duration_seconds": 100000}), "validation")
	assert.Contains(t, f.CallError("create_shot", map[string]any{"scene_id": "missing", "objective": "x", "duration_seconds": 3}), "not_found")

	var h shotplan.HierarchyOutput
	f.CallOK("get_scene_hierarchy", map[string]any{"project_id": f.ProjectID}, &h)
	assert.Len(t, h.Scenes, 1, "nothing was added")
	assert.Len(t, h.Shots, 1)
	assert.Contains(t, f.CallError("get_scene_hierarchy", map[string]any{"project_id": f.ProjectID, "scene_id": "missing"}), "not_found")
}

func TestGetShot_ReturnsTheShotSpecObjectAndNotFound(t *testing.T) {
	f := newFixture(t)
	var out shotplan.GetShotOutput
	f.CallOK("get_shot", map[string]any{"shot_id": f.ShotID}, &out)
	assert.Equal(t, f.ShotID, out.ID)
	assert.Equal(t, f.ProjectID, out.ProjectID)
	assert.NotNil(t, out.ShotSpec, "{} when the Shot has no spec")
	assert.Equal(t, "PLANNING", out.State)
	assert.Contains(t, f.CallError("get_shot", map[string]any{"shot_id": "missing"}), "not_found")
}
