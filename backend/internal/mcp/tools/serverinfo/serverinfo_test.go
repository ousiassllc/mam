package serverinfo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projects"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/serverinfo"
)

func TestGetServerInfo_CountsEveryRegisteredTool(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		serverinfo.Register(r, env.Services)
		projects.Register(r, env.Services)
	})

	var out serverinfo.Output
	env.CallOK("get_server_info", map[string]any{}, &out)
	assert.Equal(t, serverinfo.Output{Name: "mam", Version: "test", ListenAddr: "127.0.0.1:0", ToolCount: 2}, out)
}
