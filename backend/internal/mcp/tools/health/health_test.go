package health_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/health"
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
