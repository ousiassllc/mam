package settings_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
)

// restartRig is the restart service over a front that counts the restarts it was asked for (the real process is never restarted).
type restartRig struct {
	restarts atomic.Int32
	busy     atomic.Pointer[[]apprestart.Busy]
	svc      *apprestart.Service
}

func newRestartRig(t *testing.T) *restartRig {
	t.Helper()
	r := &restartRig{}
	r.svc = apprestart.New(apprestart.Deps{
		Restart: func() { r.restarts.Add(1) },
		Busy: func(context.Context) []apprestart.Busy {
			if b := r.busy.Load(); b != nil {
				return *b
			}
			return nil
		},
		Pending: restartpending.New(), BootID: "boot-1",
		Sleep: func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() },
	})
	t.Cleanup(r.svc.Close)
	return r
}

func (r *restartRig) setBusy(b ...apprestart.Busy) { r.busy.Store(&b) }

var running = apprestart.Busy{Kind: apprestart.KindJobs, Message: "生成などのジョブ 1 件（実行中 1・待機 0）"}

// Nothing restarts unless the call says so, and the request is not a way around a person's work: with work running it refuses by
// default and says what runs (#1007).
func TestRestartApp_OnlyRestartsWhenConfirmedAndNotWhileWorkRunsByDefault(t *testing.T) {
	rig := newRestartRig(t)
	env := newEnv(t, mcpkit.Settings{Restart: rig.svc})

	assert.True(t, env.Call("restart_app", map[string]any{}).IsError, "no confirm: the schema refuses it (confirm is required)")
	assert.Contains(t, env.CallError("restart_app", map[string]any{"confirm": false}), "validation:")
	assert.Contains(t, env.CallError("restart_app", map[string]any{"confirm": true, "when_busy": "sometime"}), "validation:")
	assert.Contains(t, env.CallError("restart_app", map[string]any{"confirm": true, "wait_minutes": -1}), "validation:")
	assert.Zero(t, rig.restarts.Load())

	rig.setBusy(running)
	msg := env.CallError("restart_app", map[string]any{"confirm": true})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, msg, "生成などのジョブ 1 件", "it says what runs")
	assert.Zero(t, rig.restarts.Load())

	rig.setBusy()
	var out settings.RestartOutput
	env.CallOK("restart_app", map[string]any{"confirm": true}, &out)
	assert.Equal(t, "restarting", out.State)
	assert.Equal(t, "boot-1", out.BootID)
	assert.Contains(t, out.Next, "boot_id")
	assert.EqualValues(t, 1, rig.restarts.Load())
}

func TestRestartApp_StopInterruptsJobsButNeverADownloadAndWaitReturnsAtOnce(t *testing.T) {
	rig := newRestartRig(t)
	env := newEnv(t, mcpkit.Settings{Restart: rig.svc})
	download := apprestart.Busy{Kind: apprestart.KindDownload, Message: "モデルの導入 1 件"}

	rig.setBusy(running, download)
	msg := env.CallError("restart_app", map[string]any{"confirm": true, "when_busy": "stop"})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, msg, "止めて再起動はしません")
	assert.Zero(t, rig.restarts.Load())

	var out settings.RestartOutput
	env.CallOK("restart_app", map[string]any{"confirm": true, "when_busy": "wait", "wait_minutes": 5}, &out)
	assert.Equal(t, "waiting", out.State)
	assert.NotEmpty(t, out.WaitUntil)
	require.Len(t, out.Busy, 2)
	assert.Equal(t, "jobs", out.Busy[0].Kind)
	assert.Equal(t, "download", out.Busy[1].Kind)
	assert.True(t, rig.svc.Status().Waiting)
	assert.Zero(t, rig.restarts.Load(), "waiting is not restarting")

	rig2 := newRestartRig(t)
	env2 := newEnv(t, mcpkit.Settings{Restart: rig2.svc})
	rig2.setBusy(running)
	env2.CallOK("restart_app", map[string]any{"confirm": true, "when_busy": "stop"}, &out)
	assert.Equal(t, "restarting", out.State)
	require.Len(t, out.Busy, 1, "what was interrupted is told")
	assert.EqualValues(t, 1, rig2.restarts.Load())
}

func TestRestartApp_IsUnavailableWhereTheAppCannotRestartItself(t *testing.T) {
	assert.Contains(t, newEnv(t, mcpkit.Settings{}).CallError("restart_app", map[string]any{"confirm": true}), "unavailable:")

	cannot := apprestart.New(apprestart.Deps{}) // the browser build: no front to quit
	t.Cleanup(cannot.Close)
	assert.Contains(t, newEnv(t, mcpkit.Settings{Restart: cannot}).CallError("restart_app", map[string]any{"confirm": true}), "unavailable:")
}

// The assignment is in effect at once, so the overview says it does and lists nothing as waiting for a restart (#1006).
func TestGetGlobalSettings_SaysTheLLMAssignmentAppliesAtOnce(t *testing.T) {
	s, _ := newFiles(t, false)
	env := newEnv(t, s)
	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)
	assert.False(t, ov.LLM.AppliesAtOnce, "no client runs under this service")
	assert.NotNil(t, ov.LLM.RestartPending)
	assert.Empty(t, ov.LLM.RestartPending)
}
