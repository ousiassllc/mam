package branches_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/branches"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func newEnv(t *testing.T) (*mcptest.Env, worldtest.Project) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		jobs.Register(r, env.Services)
		branches.Register(r, env.Services)
	})
	return env, worldtest.SeedProject(t, env)
}

func TestCreateBranch_SavesTheBranchPointAndListsItInTheTree(t *testing.T) {
	env, p := newEnv(t)
	var out branches.CreateOutput
	env.CallOK("create_branch", map[string]any{"shot_id": p.ShotID, "name": "もしも雨だったら"}, &out)
	assert.Equal(t, "もしも雨だったら", out.Branch.Name)
	assert.Equal(t, "shot", out.Branch.PointType, "the default is after the Shot")
	assert.Equal(t, p.ShotID, out.Branch.PointShotID)
	assert.Equal(t, "Scene 1 / Shot 1 の後", out.Branch.PointText)
	assert.Nil(t, out.Job, "no Direction: nothing is generated")
	assert.False(t, out.Started)

	var tree branches.TreeOutput
	env.CallOK("get_branch_tree", map[string]any{"project_id": p.ID}, &tree)
	require.Len(t, tree.Branches, 1)
	assert.Equal(t, out.Branch.ID, tree.Branches[0].ID)
	assert.Empty(t, tree.Branches[0].ParentID, "it hangs from the trunk")
	require.Len(t, tree.Shots, 1)
	assert.Equal(t, p.ShotID, tree.Shots[0].ShotID)

	var detail branches.DetailOutput
	env.CallOK("get_branch", map[string]any{"branch_id": out.Branch.ID}, &detail)
	assert.Equal(t, out.Branch.ID, detail.Branch.ID)
	assert.NotNil(t, detail.Scenes)
	assert.NotNil(t, detail.Shots)
	assert.NotEmpty(t, detail.Versions)
}

func TestCreateBranch_NamesAndPointTypes(t *testing.T) {
	env, p := newEnv(t)
	var first, frame, second branches.CreateOutput
	env.CallOK("create_branch", map[string]any{"shot_id": p.ShotID}, &first)
	assert.Equal(t, "Branch 1", first.Branch.Name)
	env.CallOK("create_branch", map[string]any{"shot_id": p.ShotID, "point_type": "frame", "offset_seconds": "1.5"}, &frame)
	assert.Equal(t, "frame", frame.Branch.PointType)
	assert.InDelta(t, 1.5, frame.Branch.PointOffsetSeconds, 0.001)
	env.CallOK("create_branch", map[string]any{"shot_id": p.ShotID, "point_type": "scene"}, &second)
	assert.Equal(t, "Branch 3", second.Branch.Name)
	assert.Equal(t, p.SceneID, second.Branch.PointSceneID)
}

func TestCreateBranch_RefusesWhatTheScreenRefuses(t *testing.T) {
	env, p := newEnv(t)
	assert.Contains(t, env.CallError("create_branch", map[string]any{"shot_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("create_branch", map[string]any{"shot_id": p.ShotID, "point_type": "after"}), "validation:")
	assert.Contains(t, env.CallError("create_branch", map[string]any{"shot_id": p.ShotID, "point_type": "frame"}), "validation:", "a frame needs an offset")
	assert.Contains(t, env.CallError("create_branch", map[string]any{"shot_id": p.ShotID, "point_type": "frame", "offset_seconds": "99"}), "validation:", "past the end of the Shot")
	long := make([]rune, 101)
	for i := range long {
		long[i] = 'a'
	}
	assert.Contains(t, env.CallError("create_branch", map[string]any{"shot_id": p.ShotID, "name": string(long)}), "validation:")
	assert.Contains(t, env.CallError("create_branch", map[string]any{"shot_id": p.ShotID, "direction": "逃げる"}), "unavailable", "a Direction needs generation to be configured")
	var tree branches.TreeOutput
	env.CallOK("get_branch_tree", map[string]any{"project_id": p.ID}, &tree)
	assert.Empty(t, tree.Branches, "a refused request makes no Branch")
}

func TestUpdateBranch_RenamesAndMovesTheWorldStateReference(t *testing.T) {
	env, p := newEnv(t)
	var made branches.CreateOutput
	env.CallOK("create_branch", map[string]any{"shot_id": p.ShotID}, &made)
	id := made.Branch.ID

	var renamed branches.DetailOutput
	env.CallOK("update_branch", map[string]any{"branch_id": id, "name": "別の結末"}, &renamed)
	assert.Equal(t, "別の結末", renamed.Branch.Name)
	assert.Greater(t, renamed.Branch.Version, made.Branch.Version, "the rename is appended to branch_version")

	assert.Contains(t, env.CallError("update_branch", map[string]any{"branch_id": id}), "validation:", "nothing to change")
	assert.Contains(t, env.CallError("update_branch", map[string]any{"branch_id": id, "name": " "}), "validation:")
	assert.Contains(t, env.CallError("update_branch", map[string]any{"branch_id": id, "world_state_version": 99}), "validation:", "the Branch has no such version")
	assert.Contains(t, env.CallError("update_branch", map[string]any{"branch_id": "nope", "name": "x"}), "not_found")
}

func TestGetBranch_UnknownIsNotFound(t *testing.T) {
	env, _ := newEnv(t)
	assert.Contains(t, env.CallError("get_branch", map[string]any{"branch_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("get_branch_tree", map[string]any{"project_id": "nope"}), "not_found")
}

// A Direction starts the first Shot of the Branch, which needs the AI and the video models, so the job
// is driven here by what the service persists: the Branch, its first Shot and the Shot's Takes.
func TestCreateBranchJob_FollowsTheFirstShotOfTheBranch(t *testing.T) {
	env, p := newEnv(t)
	ctx := context.Background()
	var branchID string
	ref, err := env.Registrar.Jobs.Run(ctx, branches.KindCreateBranch, p.ShotID, func(ctx context.Context) (string, error) {
		d, err := env.Services.Branch.Create(ctx, p.ShotID, service.BranchInput{})
		branchID = d.Branch.ID
		return "", err
	})
	require.NoError(t, err)
	status := func() mcpkit.JobStatus {
		var st mcpkit.JobStatus
		env.CallOK("get_job", map[string]any{"job_id": ref.JobID}, &st)
		return st
	}

	st := status()
	assert.Equal(t, mcpkit.JobFailed, st.State, "the Branch exists but no first Shot was made")
	assert.Equal(t, branchID, st.Result["branch_id"])

	worldtest.Exec(t, env, `INSERT INTO scene (id, project_id, branch_id, title, order_index) VALUES ('bsc', ?, ?, 'S', 0)`, p.ID, branchID)
	worldtest.Exec(t, env, `INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index, state) VALUES ('bsh', 'bsc', 'run', 4, 0, 'GENERATING')`)
	worldtest.Exec(t, env, `INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('bpv', 'bsh', 1, '{}')`)
	st = status()
	assert.Equal(t, mcpkit.JobRunning, st.State)
	assert.Equal(t, "bsh", st.Result["shot_id"])
	assert.Equal(t, "create_branch", st.Kind)

	worldtest.Exec(t, env, `INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('bt', 'bpv', 'kling-3.0', 'p/a.mp4')`)
	worldtest.Exec(t, env, `UPDATE shot SET state = 'EDITING' WHERE id = 'bsh'`)
	st = status()
	assert.Equal(t, mcpkit.JobSucceeded, st.State)
	assert.Equal(t, []any{"bt"}, st.Result["take_ids"])
}
