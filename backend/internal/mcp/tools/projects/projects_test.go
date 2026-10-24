package projects_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projects"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestListProjects_ReturnsTheScreensListNewestFirst(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { projects.Register(r, env.Services) })

	var out projects.ListProjectsOutput
	env.CallOK("list_projects", map[string]any{}, &out)
	assert.NotNil(t, out.Projects)
	assert.Empty(t, out.Projects, "no Project yet")

	first, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "春の新作スニーカーの 15 秒 PV"})
	require.NoError(t, err)
	second, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "カフェの開店告知ショート動画"})
	require.NoError(t, err)

	env.CallOK("list_projects", map[string]any{}, &out)
	require.Len(t, out.Projects, 2)
	assert.Equal(t, second.ID, out.Projects[0].ID)
	assert.Equal(t, first.ID, out.Projects[1].ID)
	assert.Equal(t, "auto", out.Projects[0].Mode)
	assert.NotEmpty(t, out.Projects[0].Name)
	assert.NotEmpty(t, out.Projects[0].State)
	assert.False(t, out.Projects[0].CreatedAt.IsZero())

	listed, err := env.Services.Projects.List(context.Background())
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, listed[0].ID, out.Projects[0].ID, "the tool shows the order of the service the screen uses")
}
