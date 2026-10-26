package director_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/director"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
)

func newFixture(t *testing.T) *shottest.Fixture {
	t.Helper()
	return shottest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		jobs.Register(r, env.Services)
		director.Register(r, env.Services)
	}, shottest.Options{})
}

func TestSetProjectMode_SwitchesAndRejectsUnknownModes(t *testing.T) {
	f := newFixture(t)
	var out director.SetModeOutput
	f.CallOK("set_project_mode", map[string]any{"project_id": f.ProjectID, "mode": "auto"}, &out)
	assert.Equal(t, "auto", out.Mode)
	f.CallOK("set_project_mode", map[string]any{"project_id": f.ProjectID, "mode": "director"}, &out)
	assert.Equal(t, "director", out.Mode)

	assert.Contains(t, f.CallError("set_project_mode", map[string]any{"project_id": f.ProjectID, "mode": "turbo"}), "validation")
	assert.Contains(t, f.CallError("set_project_mode", map[string]any{"project_id": "missing", "mode": "auto"}), "not_found")
}

func TestProjectLocks_SetAndReadStoryAndMusic(t *testing.T) {
	f := newFixture(t)
	var out director.LocksOutput
	f.CallOK("get_project_locks", map[string]any{"project_id": f.ProjectID}, &out)
	assert.Equal(t, "auto", out.Locks["story"].Mode)
	assert.Equal(t, "auto", out.Locks["music"].Mode)

	f.CallOK("set_project_lock", map[string]any{"project_id": f.ProjectID, "parameter": "story", "mode": "lock"}, &out)
	assert.Equal(t, "lock", out.Locks["story"].Mode)
	f.CallOK("get_project_locks", map[string]any{"project_id": f.ProjectID}, &out)
	assert.Equal(t, "lock", out.Locks["story"].Mode)
	assert.Equal(t, "auto", out.Locks["music"].Mode)

	assert.Contains(t, f.CallError("set_project_lock", map[string]any{"project_id": f.ProjectID, "parameter": "camera", "mode": "lock"}), "validation", "a Shot parameter is not a Project lock")
	assert.Contains(t, f.CallError("set_project_lock", map[string]any{"project_id": f.ProjectID, "parameter": "story", "mode": "freeze"}), "validation")
	assert.Contains(t, f.CallError("set_project_lock", map[string]any{"project_id": "missing", "parameter": "story", "mode": "lock"}), "not_found")
}

func TestSetShotLock_VideoModelValueAndValidation(t *testing.T) {
	f := newFixture(t)
	var out director.LocksOutput
	f.CallOK("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "camera", "mode": "lock"}, &out)
	assert.Equal(t, "lock", out.Locks["camera"].Mode)
	f.CallOK("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "video_model", "mode": "manual", "value": "kling-3.0"}, &out)
	assert.Equal(t, director.Lock{Mode: "manual", Value: "kling-3.0"}, out.Locks["video_model"])
	assert.Len(t, out.Locks, 10, "every Shot parameter is listed")

	assert.Contains(t, f.CallError("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "video_model", "mode": "manual"}), "validation", "MANUAL needs a model")
	assert.Contains(t, f.CallError("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "video_model", "mode": "manual", "value": "no-such-model"}), "validation")
	assert.Contains(t, f.CallError("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "story", "mode": "lock"}), "validation")
	assert.Contains(t, f.CallError("set_shot_lock", map[string]any{"shot_id": "missing", "parameter": "camera", "mode": "lock"}), "not_found")

	// A LOCKed value is not replaced: video_model stays LOCK, only the value would change.
	f.CallOK("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "video_model", "mode": "lock", "value": "kling-3.0"}, &out)
	assert.Contains(t, f.CallError("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "video_model", "mode": "lock", "value": "seedance-2.0"}), "locked")
	f.CallOK("get_shot_inspector", map[string]any{"shot_id": f.ShotID}, new(director.InspectorOutput))
}

func TestUpdateShotSpec_SavesAValidSpecAndRejectsAnInvalidOrLockedOne(t *testing.T) {
	f := newFixture(t)
	spec := map[string]any{
		"shot_objective": "The woman starts running", "character": map[string]any{"id": "Mira", "identity": "locked"},
		"camera": map[string]any{"framing": "medium"},
	}
	var out director.UpdateSpecOutput
	f.CallOK("update_shot_spec", map[string]any{"shot_id": f.ShotID, "shot_spec": spec}, &out)
	assert.Equal(t, "4.0s", out.Spec["duration"], "the duration follows the Shot")

	var insp director.InspectorOutput
	f.CallOK("get_shot_inspector", map[string]any{"shot_id": f.ShotID}, &insp)
	assert.Equal(t, "The woman starts running", insp.Spec["shot_objective"])
	assert.Equal(t, f.SceneID, insp.SceneID)
	assert.Equal(t, "director", insp.ProjectMode)
	assert.NotEmpty(t, insp.Versions, "the edit is a saved version")

	assert.Contains(t, f.CallError("update_shot_spec", map[string]any{"shot_id": f.ShotID, "shot_spec": map[string]any{"unknown_key": 1}}), "validation")
	japanese := map[string]any{"shot_objective": "主人公が走り出す", "character": map[string]any{"id": "Mira", "identity": "locked"}, "camera": map[string]any{"framing": "medium"}}
	assert.Contains(t, f.CallError("update_shot_spec", map[string]any{"shot_id": f.ShotID, "shot_spec": japanese}), "validation", "a spec must be in English")
	assert.Contains(t, f.CallError("update_shot_spec", map[string]any{"shot_id": "missing", "shot_spec": spec}), "not_found")

	f.CallOK("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "camera", "mode": "lock"}, nil)
	changed := map[string]any{
		"shot_objective": "The woman starts running", "character": map[string]any{"id": "Mira", "identity": "locked"},
		"camera": map[string]any{"framing": "close_up"},
	}
	assert.Contains(t, f.CallError("update_shot_spec", map[string]any{"shot_id": f.ShotID, "shot_spec": changed}), "locked")
	f.CallOK("get_shot_inspector", map[string]any{"shot_id": f.ShotID}, &insp)
	require.NotNil(t, insp.Spec["camera"])
	assert.Equal(t, "medium", insp.Spec["camera"].(map[string]any)["framing"], "the LOCKed Camera was not changed")
}

func TestSendDirectorCommand_ValidationNotFoundAndNoOperationDoesNotStartAJob(t *testing.T) {
	f := newFixture(t)
	assert.Contains(t, f.CallError("send_director_command", map[string]any{"shot_id": f.ShotID, "content": "  "}), "validation")
	assert.Contains(t, f.CallError("send_director_command", map[string]any{"shot_id": "missing", "content": "寄って"}), "not_found")

	// The fake Command Interpreter holds no operation: the direction is answered and nothing is regenerated.
	var out director.CommandOutput
	f.CallOK("send_director_command", map[string]any{"shot_id": f.ShotID, "content": "もう少しカメラを寄せて"}, &out)
	assert.False(t, out.Started)
	assert.Empty(t, out.JobID)
	assert.Equal(t, f.ShotID, out.Inspector.Shot.ID)
}

func TestSendDirectorCommand_ARegenerationReturnsAJobThatGetJobFollows(t *testing.T) {
	f := shottest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		jobs.Register(r, env.Services)
		director.Register(r, env.Services)
	}, shottest.Options{Command: `{"type":"regenerate","changes":["camera"],"instruction":"Move the camera closer and keep it steady."}`})
	f.Generate(t)

	var out director.CommandOutput
	f.CallOK("send_director_command", map[string]any{"shot_id": f.ShotID, "content": "もう少し寄って"}, &out)
	require.True(t, out.Started)
	require.NotEmpty(t, out.JobID)

	var st mcpkit.JobStatus
	require.Eventually(t, func() bool {
		f.CallOK("get_job", map[string]any{"job_id": out.JobID}, &st)
		return st.State.Final()
	}, 30*time.Second, 10*time.Millisecond)
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, f.ShotID, st.Target)
}

func TestSendDirectorCommand_ALockedParameterIsRefusedAndNothingStarts(t *testing.T) {
	f := shottest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		jobs.Register(r, env.Services)
		director.Register(r, env.Services)
	}, shottest.Options{Command: `{"type":"regenerate","changes":["camera"],"instruction":"Move the camera closer."}`})
	f.Generate(t)
	f.CallOK("set_shot_lock", map[string]any{"shot_id": f.ShotID, "parameter": "camera", "mode": "lock"}, nil)
	before := f.Count(t, `SELECT COUNT(*) FROM job WHERE kind = 'generate'`)

	assert.Contains(t, f.CallError("send_director_command", map[string]any{"shot_id": f.ShotID, "content": "もう少し寄って"}), "locked")
	f.WaitIdle(t)
	assert.Equal(t, before, f.Count(t, `SELECT COUNT(*) FROM job WHERE kind = 'generate'`), "no regeneration was queued")
}
