// Package editorreview has the one test that ties the three tool packages of the editor and review
// domain (editor, editorstage, review) to their spec files.
package editorreview_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editor"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editorstage"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/review"
)

// noRoute is the one tool that reads data the screen shows as a part of another page: the voices of the
// editor screen are in get_editor_view too.
const noRoute = "list_voice_clones"

var specFiles = []string{"mcp-tools-editor-review.md", "mcp-tools-editor-review-reviews.md"}

var (
	routeRow = regexp.MustCompile("^\\| `(?:GET|POST|PUT|DELETE|PATCH) [^`]+`[^|]*\\| (.+) \\|$")
	toolRow  = regexp.MustCompile("^\\| `([a-z][a-z0-9_]+)` \\|")
	backtick = regexp.MustCompile("`([^`]+)`")
)

// readSpecs returns the tools the coverage tables map routes to, the upload purposes they name, and the
// tools the tool tables describe.
func readSpecs(t *testing.T) (covered, purposes, described map[string]bool) {
	t.Helper()
	covered, purposes, described = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, name := range specFiles {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "spec", "api", name))
		require.NoError(t, err)
		for _, line := range strings.Split(string(raw), "\n") {
			if m := toolRow.FindStringSubmatch(line); m != nil {
				described[m[1]] = true
			}
			m := routeRow.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			first := backtick.FindStringSubmatch(m[1])
			require.NotNil(t, first, "no tool in the row: %s", line)
			if purpose, ok := strings.CutPrefix(first[1], "upload: "); ok {
				purposes[purpose] = true
			} else {
				covered[first[1]] = true
			}
		}
	}
	return covered, purposes, described
}

func TestSpecCoverage_EveryToolAndPurposeTheRouteTablesNameIsRegisteredAndEveryToolIsInTheSpec(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		editor.Register(r, env.Services)
		editorstage.Register(r, env.Services)
		review.Register(r, env.Services)
	})
	registered := map[string]bool{}
	for _, n := range env.Registrar.ToolNames() {
		registered[n] = true
	}
	covered, purposes, described := readSpecs(t)
	require.NotEmpty(t, covered, "the coverage tables were not read")

	for name := range covered {
		assert.True(t, registered[name], "the spec maps a route to %q, which is not registered", name)
	}
	for name := range registered {
		assert.True(t, described[name], "%q is registered but has no row in the tool tables of the spec", name)
		assert.True(t, covered[name] || name == noRoute, "%q is registered but no route of the coverage tables maps to it", name)
	}
	for name := range described {
		assert.True(t, registered[name], "the spec describes %q, which is not registered", name)
	}

	registeredPurposes := map[string]bool{}
	for _, s := range env.Registrar.Files.UploadPurposes() {
		registeredPurposes[s.Purpose] = true
	}
	assert.Equal(t, registeredPurposes, purposes, "the upload purposes of the coverage tables and the registered ones")
}
