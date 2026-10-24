package analytics_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/analytics"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestGetProductionMetrics_ProjectAndAllProjects(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { analytics.Register(r, env.Services) })

	var all analytics.Output
	env.CallOK("get_production_metrics", map[string]any{}, &all)
	assert.Empty(t, all.ProjectID, "no project_id is the metrics of every Project")
	assert.Zero(t, all.Metrics.Projects)
	assert.False(t, all.Metrics.NorthStar.Defined, "nothing to measure yet is not 0%")

	first, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "春の新作スニーカーの 15 秒 PV"})
	require.NoError(t, err)
	_, err = env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "カフェの開店告知ショート動画"})
	require.NoError(t, err)

	env.CallOK("get_production_metrics", map[string]any{}, &all)
	assert.Equal(t, 2, all.Metrics.Projects)

	var one analytics.Output
	env.CallOK("get_production_metrics", map[string]any{"project_id": first.ID}, &one)
	assert.Equal(t, first.ID, one.ProjectID)
	assert.Equal(t, first.Name, one.ProjectName)
	assert.Equal(t, 1, one.Metrics.Projects)

	want, err := env.Services.Analytics.Metrics(context.Background(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, want.Metrics.Started, one.Metrics.Started, "the tool reports what the screen's service computes")

	assert.Contains(t, env.CallError("get_production_metrics", map[string]any{"project_id": "missing"}), "not_found:")
}
