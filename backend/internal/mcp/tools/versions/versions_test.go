package versions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/versions"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldstate"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
)

func newEnv(t *testing.T) (*mcptest.Env, worldtest.Project) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		versions.Register(r, env.Services)
		worldstate.Register(r, env.Services)
	})
	return env, worldtest.Seed(t, env)
}

func TestWorldStateVersions_ListAndRestoreAppendACopy(t *testing.T) {
	env, p := newEnv(t)
	edit := func(wardrobe string) {
		env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "wardrobe": wardrobe}, nil)
	}
	edit("black_coat")
	edit("red_dress")

	var list versions.List
	env.CallOK("list_world_state_versions", map[string]any{"project_id": p.ID}, &list)
	require.Len(t, list.Versions, 2)
	assert.Equal(t, 2, list.Versions[0].Number, "newest first")

	var view worldstate.View
	env.CallOK("restore_world_state_version", map[string]any{"project_id": p.ID, "version_number": 1}, &view)
	assert.Equal(t, "black_coat", view.Characters[0].Wardrobe)
	require.Len(t, view.Versions, 3, "a restore appends a version; nothing is deleted")

	assert.Contains(t, env.CallError("restore_world_state_version", map[string]any{"project_id": p.ID, "version_number": 99}), "not_found")
	assert.Contains(t, env.CallError("restore_world_state_version", map[string]any{"project_id": p.ID, "version_number": 0}), "validation:")
	assert.Contains(t, env.CallError("restore_world_state_version", map[string]any{"project_id": p.ID, "version_number": 1, "branch_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("list_world_state_versions", map[string]any{"project_id": "nope"}), "not_found")
}

func TestRestoreWorldStateVersion_IsRefusedWhenItWouldChangeALockedField(t *testing.T) {
	env, p := newEnv(t)
	env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "wardrobe": "black_coat"}, nil)
	env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "wardrobe": "red_dress"}, nil)
	env.CallOK("set_world_field_lock", map[string]any{"project_id": p.ID, "target_type": "character", "target_id": "c1", "parameter": "wardrobe", "mode": "lock"}, nil)

	assert.Contains(t, env.CallError("restore_world_state_version", map[string]any{"project_id": p.ID, "version_number": 1}), "locked")
	var view worldstate.View
	env.CallOK("get_world_state", map[string]any{"project_id": p.ID}, &view)
	assert.Equal(t, "red_dress", view.Characters[0].Wardrobe, "nothing was written")
	assert.Len(t, view.Versions, 2)
}

func TestShotVersions_ListAndRestore(t *testing.T) {
	env, p := newEnv(t)
	var list versions.List
	env.CallOK("list_shot_versions", map[string]any{"shot_id": p.ShotID}, &list)
	assert.NotNil(t, list.Versions)
	assert.Contains(t, env.CallError("list_shot_versions", map[string]any{"shot_id": "nope"}), "not_found")

	worldtest.Exec(t, env, `INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', ?, 1, '{"shot_objective":"first"}')`, p.ShotID)
	env.CallOK("list_shot_versions", map[string]any{"shot_id": p.ShotID}, &list)
	require.Len(t, list.Versions, 1)
	assert.Equal(t, "first", list.Versions[0].Summary)

	assert.Contains(t, env.CallError("restore_shot_version", map[string]any{"shot_id": p.ShotID, "version_number": 7}), "not_found")
	assert.Contains(t, env.CallError("restore_shot_version", map[string]any{"shot_id": p.ShotID, "version_number": -1}), "validation:")
	assert.Contains(t, env.CallError("restore_shot_version", map[string]any{"shot_id": "nope", "version_number": 1}), "not_found")

	var restored versions.RestoredShot
	env.CallOK("restore_shot_version", map[string]any{"shot_id": p.ShotID, "version_number": 1}, &restored)
	assert.Len(t, restored.Versions, 2, "the restore is a new version")

	worldtest.Exec(t, env, `UPDATE shot SET state = 'GENERATING' WHERE id = ?`, p.ShotID)
	assert.Contains(t, env.CallError("restore_shot_version", map[string]any{"shot_id": p.ShotID, "version_number": 1}), "conflict", "a Shot being generated")
	assert.Equal(t, 2, worldtest.Count(t, env, `SELECT COUNT(*) FROM prompt_version WHERE shot_id = ?`, p.ShotID))
}

func TestBranchVersions_HistoryAndBothRestores(t *testing.T) {
	env, p := newEnv(t)
	worldtest.Exec(t, env, `INSERT INTO branch (id, project_id, branch_point_type, branch_point_shot_id, name) VALUES ('b1', ?, 'shot', ?, 'renamed')`, p.ID, p.ShotID)
	worldtest.Exec(t, env, `INSERT INTO branch_version (id, branch_id, version_number, name) VALUES ('v1', 'b1', 1, 'original'), ('v2', 'b1', 2, 'renamed')`)
	worldtest.Exec(t, env, `INSERT INTO world_state_version (id, project_id, branch_id, version_number, state) VALUES ('w1', ?, 'b1', 1, '{"characters":[{"id":"x","name":"A"}]}'), ('w2', ?, 'b1', 2, '{}')`, p.ID, p.ID)

	var h versions.BranchHistory
	env.CallOK("get_branch_versions", map[string]any{"branch_id": "b1"}, &h)
	require.Len(t, h.Versions, 2)
	assert.Equal(t, "renamed", h.Versions[0].Summary)
	assert.Len(t, h.World, 2)

	env.CallOK("restore_branch_version", map[string]any{"branch_id": "b1", "version_number": 1}, &h)
	assert.Len(t, h.Versions, 3)
	assert.Equal(t, "original", h.Versions[0].Summary, "the restored name is the newest version")
	assert.Equal(t, 1, worldtest.Count(t, env, `SELECT COUNT(*) FROM branch WHERE id = 'b1' AND name = 'original'`))

	env.CallOK("restore_branch_world_state_version", map[string]any{"branch_id": "b1", "version_number": 1}, &h)
	assert.Len(t, h.World, 3)

	for _, tool := range []string{"restore_branch_version", "restore_branch_world_state_version"} {
		assert.Contains(t, env.CallError(tool, map[string]any{"branch_id": "b1", "version_number": 9}), "not_found", tool)
		assert.Contains(t, env.CallError(tool, map[string]any{"branch_id": "nope", "version_number": 1}), "not_found", tool)
		assert.Contains(t, env.CallError(tool, map[string]any{"branch_id": "b1", "version_number": 0}), "validation:", tool)
	}
	assert.Contains(t, env.CallError("get_branch_versions", map[string]any{"branch_id": "nope"}), "not_found")
}

func TestRestoreBranchWorldState_IsRefusedWhenItWouldChangeALockedField(t *testing.T) {
	env, p := newEnv(t)
	worldtest.Exec(t, env, `INSERT INTO branch (id, project_id, branch_point_type, branch_point_shot_id, name) VALUES ('b1', ?, 'shot', ?, 'b')`, p.ID, p.ShotID)
	worldtest.Exec(t, env, `INSERT INTO character (id, project_id, branch_id, name, wardrobe) VALUES ('c1b', ?, 'b1', 'Mio', 'now')`, p.ID)
	worldtest.Exec(t, env, `INSERT INTO world_state_version (id, project_id, branch_id, version_number, state) VALUES ('w1', ?, 'b1', 1, '{"characters":[{"id":"c1b","name":"Mio","wardrobe":"before"}]}')`, p.ID)
	env.CallOK("set_world_field_lock", map[string]any{"project_id": p.ID, "branch_id": "b1", "target_type": "character", "target_id": "c1b", "parameter": "wardrobe", "mode": "lock"}, nil)

	assert.Contains(t, env.CallError("restore_branch_world_state_version", map[string]any{"branch_id": "b1", "version_number": 1}), "locked")
	assert.Equal(t, 1, worldtest.Count(t, env, `SELECT COUNT(*) FROM world_state_version WHERE branch_id = 'b1'`), "nothing was appended")
}
