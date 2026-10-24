package shotgen_test

import (
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/director"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotgen"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotplan"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/takes"
)

// specFile is the spec whose coverage table (route -> tool) this domain keeps.
const specFile = "../../../../../spec/api/mcp-tools-shots-generation.md"

// A coverage row is "| `METHOD /path` | `tool` ..." or "| `METHOD /path` | none: reason".
var (
	coverageRow = regexp.MustCompile("(?m)^\\| `(GET|POST|PUT|DELETE) ([^`]+)` \\| (?:`([a-z0-9_]+)`|none: )")
	toolRow     = regexp.MustCompile("(?m)^\\| `([a-z0-9_]+)` \\|")
)

// Every tool the spec names is registered, and every tool these packages register is in the spec: the
// coverage table cannot drift from the code.
func TestSpecCoverage_EveryListedToolIsRegisteredAndEveryRegisteredToolIsListed(t *testing.T) {
	raw, err := os.ReadFile(specFile)
	require.NoError(t, err)
	spec := string(raw)

	routed := map[string]bool{}
	rows := coverageRow.FindAllStringSubmatch(spec, -1)
	require.NotEmpty(t, rows, "the coverage table was not found")
	for _, m := range rows {
		if m[3] != "" {
			routed[m[3]] = true
		}
	}
	listed := map[string]bool{}
	for _, m := range toolRow.FindAllStringSubmatch(spec, -1) {
		listed[m[1]] = true
	}

	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		shotplan.Register(r, env.Services)
		director.Register(r, env.Services)
		shotgen.Register(r, env.Services)
		takes.Register(r, env.Services)
	})
	registered := env.Registrar.ToolNames()

	for name := range routed {
		assert.Contains(t, registered, name, "the spec maps a route to %s, which is not registered", name)
	}
	var unlisted []string
	for _, name := range registered {
		if !listed[name] {
			unlisted = append(unlisted, name)
		}
	}
	sort.Strings(unlisted)
	assert.Empty(t, unlisted, "registered tools missing from the tool tables of the spec")
}
