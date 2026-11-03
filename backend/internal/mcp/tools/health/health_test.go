package health_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/health"
	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
)

func TestGetHealth_ReportsTheDatabaseAndDoesNotFailWhenItIsDown(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { health.Register(r, env.Services) })

	var out health.Output
	env.CallOK("get_health", map[string]any{}, &out)
	assert.True(t, out.OK)
	assert.Equal(t, "ok", out.Database)

	require.NoError(t, env.DB.Close())
	env.CallOK("get_health", map[string]any{}, &out)
	assert.False(t, out.OK, "an unusable database is the answer, not a tool error")
	assert.Equal(t, "unavailable", out.Database)
}

// After a restart_app the run has another boot_id, which is how the caller sees the app came back; what waits for a restart is listed.
func TestGetHealth_TellsTheRunAndWhatWaitsForARestart(t *testing.T) {
	pending := restartpending.New()
	pending.Set("ffmpeg", []string{"ffmpeg のパス"})
	svc := apprestart.New(apprestart.Deps{Restart: func() {}, Pending: pending, BootID: "boot-7", StartedAt: time.Date(2026, 11, 3, 1, 2, 3, 0, time.UTC)})
	t.Cleanup(svc.Close)
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		r.Settings.Restart = svc
		health.Register(r, env.Services)
	})

	var out health.Output
	env.CallOK("get_health", map[string]any{}, &out)

	assert.Equal(t, "boot-7", out.BootID)
	assert.Equal(t, "2026-11-03T01:02:03Z", out.StartedAt)
	assert.Equal(t, []string{"ffmpeg のパス"}, out.RestartPending)
	assert.False(t, out.RestartWaiting)
}

func TestGetHealth_WithoutARestartServiceHasNoRunAndAnEmptyList(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { health.Register(r, env.Services) })
	var out health.Output
	env.CallOK("get_health", map[string]any{}, &out)
	assert.Empty(t, out.BootID)
	assert.NotNil(t, out.RestartPending)
}
