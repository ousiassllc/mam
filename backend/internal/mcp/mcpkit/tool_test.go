package mcpkit_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

type echoIn struct {
	Text string `json:"text" jsonschema:"what to echo"`
}
type echoOut struct {
	Echo  string   `json:"echo"`
	Items []string `json:"items"`
}

func registerDemo(_ *mcptest.Env, r *mcpkit.Registrar) {
	mcpkit.Read(r, "echo_text", "echo", func(_ context.Context, in echoIn) (echoOut, error) {
		return echoOut{Echo: in.Text}, nil
	})
	mcpkit.Write(r, "fail_text", "always fails", func(_ context.Context, in echoIn) (echoOut, error) {
		switch in.Text {
		case "missing":
			return echoOut{}, fmt.Errorf("project p1: %w", service.ErrNotFound)
		case "invalid":
			return echoOut{}, &service.ValidationError{Field: "text", Message: "短すぎます"}
		}
		return echoOut{}, fmt.Errorf("open /secret/path: boom")
	})
	mcpkit.Destroy(r, "remove_text", "removes", func(_ context.Context, _ echoIn) (echoOut, error) { return echoOut{}, nil }, mcpkit.Idempotent())
}

func TestTools_AnnotationsSchemaAndErrorsReachTheClient(t *testing.T) {
	env := mcptest.New(t, registerDemo)

	listed, err := env.Client.ListTools(context.Background(), nil)
	require.NoError(t, err)
	tools := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}
	read, write, destroy := tools["echo_text"].Annotations, tools["fail_text"].Annotations, tools["remove_text"].Annotations
	assert.True(t, read.ReadOnlyHint)
	assert.False(t, *read.OpenWorldHint, "the MCP default is open world; a mam tool says closed")
	assert.False(t, write.ReadOnlyHint)
	assert.False(t, *write.DestructiveHint)
	assert.True(t, *destroy.DestructiveHint)
	assert.True(t, destroy.IdempotentHint)
	assert.NotNil(t, tools["echo_text"].InputSchema)
	assert.NotNil(t, tools["echo_text"].OutputSchema)

	var out echoOut
	env.CallOK("echo_text", map[string]any{"text": "hi"}, &out)
	assert.Equal(t, "hi", out.Echo)
	assert.Nil(t, out.Items, "a nil list is accepted by the output schema and arrives as null: tools make lists with make() to send []")

	assert.Equal(t, "not_found: 対象が見つかりません", env.CallError("fail_text", map[string]any{"text": "missing"}))
	assert.Equal(t, "validation: invalid input: text: 短すぎます", env.CallError("fail_text", map[string]any{"text": "invalid"}))
	internal := env.CallError("fail_text", map[string]any{"text": "other"})
	assert.Contains(t, internal, "internal:")
	assert.NotContains(t, internal, "/secret/path")
	assert.True(t, env.Call("echo_text", map[string]any{"text": 5}).IsError, "arguments are validated against the input schema")
}

func TestRegistrar_RefusesBadAndDuplicateToolNames(t *testing.T) {
	r := mcpkit.NewRegistrar(mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil), mcpkit.Kit{})
	h := func(context.Context, echoIn) (echoOut, error) { return echoOut{}, nil }

	for _, bad := range []string{"list", "ListProjects", "list-projects", "list_projects_", "_x", "list__x", "x y", ""} {
		assert.Panics(t, func() { mcpkit.Read(r, bad, "d", h) }, "name %q", bad)
	}
	assert.Panics(t, func() { mcpkit.Read(r, "list_things", "", h) }, "a tool needs a description")

	mcpkit.Read(r, "list_things", "d", h)
	mcpkit.Read(r, "get_thing2", "d", h)
	assert.Panics(t, func() { mcpkit.Write(r, "list_things", "d", h) })
	assert.Equal(t, []string{"get_thing2", "list_things"}, r.ToolNames())
}
