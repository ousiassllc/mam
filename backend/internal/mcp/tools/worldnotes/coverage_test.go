package worldnotes_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldnotes"
)

const specPath = "../../../../../spec/api/mcp-tools-world-notes-coverage.md"

var (
	routeRow = regexp.MustCompile("^\\| `((?:GET|POST|PUT|DELETE) [^`]+)` \\| (.+?) \\|$")
	toolName = regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`")
	// remark is a parenthesized note after a tool name; the names inside it are not tools of the row.
	remark = regexp.MustCompile(`（[^）]*）`)
)

// The coverage table of the spec names a tool for every route of the domain: each one must exist, and
// every tool of this domain must be named by a route.
func TestSpecCoverageTableNamesOnlyRegisteredTools(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		files.Register(r, env.Services) // create_upload_ticket covers the reference upload
		worldnotes.Register(r, env.Services)
	})
	registered := map[string]bool{}
	for _, n := range env.Registrar.ToolNames() {
		registered[n] = true
	}

	raw, err := os.ReadFile(specPath)
	require.NoError(t, err)
	routes, tools := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		m := routeRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		routes[m[1]] = true
		if strings.HasPrefix(m[2], "none:") {
			continue
		}
		names := toolName.FindAllStringSubmatch(remark.ReplaceAllString(m[2], ""), -1)
		require.NotEmpty(t, names, "row of %s names no tool: %s", m[1], m[2])
		for _, n := range names {
			tools[n[1]] = true
			assert.True(t, registered[n[1]], "%s (%s) is not registered", n[1], m[1])
		}
	}
	assert.GreaterOrEqual(t, len(routes), 35, "the table lists every route of the domain")

	// every tool this package registers is reachable from some route
	for _, n := range env.Registrar.ToolNames() {
		if n == "create_upload_ticket" || n == "list_upload_purposes" || n == "get_asset_download_url" {
			continue
		}
		assert.True(t, tools[n], "%s is registered but no route of the coverage table names it", n)
	}
}
