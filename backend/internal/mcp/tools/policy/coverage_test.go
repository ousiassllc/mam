package policy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/analytics"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/brand"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/health"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/policy"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
)

var (
	toolRef  = regexp.MustCompile("`([a-z][a-z0-9]*(?:_[a-z0-9]+)+)`")
	routeRow = regexp.MustCompile("^\\| `(GET|POST|PUT|DELETE|PATCH) ([^`]+)` \\| (.+) \\|$")
)

// TestSpecCoverage_EveryRouteMapsToARegisteredToolAndEveryToolIsDocumented reads the coverage table of
// spec/api/mcp-tools-settings-policy.md: a tool named there must exist (the foundation's credential tools
// included), a route without a tool must say why, and every tool of these packages must be in the spec.
func TestSpecCoverage_EveryRouteMapsToARegisteredToolAndEveryToolIsDocumented(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		policy.Register(r, env.Services)
		brand.Register(r, env.Services)
		analytics.Register(r, env.Services)
		health.Register(r, env.Services)
		settings.Register(r, env.Services)
	})
	registered := env.Registrar.ToolNames()
	require.NotEmpty(t, registered)
	foundation := []string{"set_credential", "delete_credential", "list_credentials"}

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "spec", "api", "mcp-tools-settings-policy.md")) // go test runs in the package directory
	require.NoError(t, err)
	spec := string(raw)

	routes, mapped := 0, map[string]bool{}
	for _, line := range strings.Split(spec, "\n") {
		m := routeRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		routes++
		cell := m[3]
		if strings.HasPrefix(cell, "none:") {
			assert.Greater(t, len(strings.TrimSpace(strings.TrimPrefix(cell, "none:"))), 0, "%s %s: say why there is no tool", m[1], m[2])
			continue
		}
		names := toolRef.FindAllStringSubmatch(cell, -1)
		require.NotEmpty(t, names, "%s %s names no tool", m[1], m[2])
		for _, n := range names {
			mapped[n[1]] = true
			assert.True(t, slices.Contains(registered, n[1]) || slices.Contains(foundation, n[1]),
				"%s %s: %s is not a registered tool", m[1], m[2], n[1])
		}
	}
	assert.GreaterOrEqual(t, routes, 25, "the coverage table lists the routes of the domain")

	for _, name := range registered {
		assert.Contains(t, spec, "| `"+name+"` |", "%s is registered but has no row in the spec", name)
		assert.True(t, mapped[name], "%s is registered but no route of the coverage table maps to it", name)
	}
}
