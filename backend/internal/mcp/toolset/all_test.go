package toolset_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/toolset"
)

// Every tool package registers into one namespace: a name served by two packages panics at start-up, and
// a tool missing from spec/api/mcp-tools*.md is a tool nobody can learn about.
func TestRegisterAll_NamesAreUniqueAndEveryToolIsInTheSpec(t *testing.T) {
	var names []string
	mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		toolset.Register(r, env.Services)
		names = r.ToolNames()
	}, mcptest.WithJobQueue)
	require.NotEmpty(t, names)

	files, err := filepath.Glob("../../../../spec/api/mcp-tools*.md")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	var spec strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		spec.Write(b)
	}
	for _, name := range names {
		assert.Contains(t, spec.String(), "`"+name+"`", "tool %s is not documented under spec/api/mcp-tools*.md", name)
	}
}
