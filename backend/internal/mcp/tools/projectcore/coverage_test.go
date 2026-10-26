package projectcore_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/chat"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projectbundle"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projectcore"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projects"
)

const specFile = "../../../../../spec/api/mcp-tools-projects-chat.md"

var (
	routeRow = regexp.MustCompile("^\\| `((?:GET|POST|PUT|DELETE) [^`]+)`(?:[^|]*) \\| (.+) \\|$")
	toolRow  = regexp.MustCompile("^\\| `([a-z]+(?:_[a-z]+)+)` \\|")
	firstTok = regexp.MustCompile("^`([a-z]+(?:_[a-z]+)+)`")
)

// TestSpec_EveryToolAndEveryRouteOfTheSpecIsRegistered keeps spec/api/mcp-tools-projects-chat.md and the
// registered tools in step: each tool of the tool table and each tool the route table points at exists, and
// every tool of this domain's packages is listed in the spec.
func TestSpec_EveryToolAndEveryRouteOfTheSpecIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(specFile)
	require.NoError(t, err)

	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		projects.Register(r, env.Services) // list_projects (foundation)
		files.Register(r, env.Services)    // get_asset_download_url, import_bundle (foundation)
		projectcore.Register(r, env.Services)
		chat.Register(r, env.Services)
		projectbundle.Register(r, env.Services)
	})
	registered := map[string]bool{}
	for _, n := range env.Registrar.ToolNames() {
		registered[n] = true
	}

	var documented, routes int
	section := ""
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
		}
		inRoutes := section == "ルートとの対応"
		if m := toolRow.FindStringSubmatch(line); m != nil && section == "ツール" {
			documented++
			assert.True(t, registered[m[1]], "the spec describes %s but no package registers it", m[1])
		}
		if m := routeRow.FindStringSubmatch(line); m != nil && inRoutes {
			routes++
			if strings.HasPrefix(m[2], "none:") {
				continue
			}
			tool := firstTok.FindStringSubmatch(m[2])
			require.NotNil(t, tool, "route %s names no tool", m[1])
			assert.True(t, registered[tool[1]], "route %s -> %s is not registered", m[1], tool[1])
		}
	}
	assert.GreaterOrEqual(t, routes, 13, "the route table was not parsed")
	// The tools of the three packages: foundation tools (list_projects ...) are not counted.
	assert.Equal(t, len(env.Registrar.ToolNames())-foundationTools, documented, "a registered tool of this domain is missing from the spec table")
}

// foundationTools are the tools of tools/projects and tools/files registered above.
const foundationTools = 4 // list_projects, list_upload_purposes, create_upload_ticket, get_asset_download_url
