package mcpkit_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/logging"
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

// An internal error has a correlation id: the log line of its cause and the error the agent gets carry the
// same one, and the cause stays out of the message.
func TestTools_AnInternalErrorCarriesACorrelationIdThatFindsItsLogLine(t *testing.T) {
	env := mcptest.New(t, registerDemo)

	first := env.CallError("fail_text", map[string]any{"text": "other"})
	second := env.CallError("fail_text", map[string]any{"text": "other"})
	idOf := func(msg string) string {
		m := errorIDPattern.FindStringSubmatch(msg)
		require.Len(t, m, 2, msg)
		return m[1]
	}
	id1, id2 := idOf(first), idOf(second)
	assert.NotEqual(t, id1, id2, "one id per failure")
	assert.True(t, strings.HasPrefix(first, "internal: "), first)
	assert.NotContains(t, first, "/secret/path")

	res, err := logging.Reader{Dir: env.LogDir}.Read(context.Background(), logging.Query{
		Limit: 10, MinLevel: slog.LevelDebug, Attrs: map[string]string{mcpkit.ErrorIDKey: id1}})
	require.NoError(t, err)
	require.Len(t, res.Entries, 1, "exactly the line of this failure")
	line := res.Entries[0]
	assert.Equal(t, "ERROR", line.Level)
	assert.Equal(t, "mcp tool failed", line.Message)
	assert.Equal(t, "fail_text", line.Attrs["tool"])
	assert.Contains(t, line.Attrs["err"], "open /secret/path: boom", "the cause is in the log")

	// Errors the tools know carry no id and write no line.
	assert.NotContains(t, env.CallError("fail_text", map[string]any{"text": "missing"}), "error_id")
	all, err := logging.Reader{Dir: env.LogDir}.Read(context.Background(), logging.Query{Limit: 10, MinLevel: slog.LevelDebug})
	require.NoError(t, err)
	assert.Len(t, all.Entries, 2)
}

var errorIDPattern = regexp.MustCompile(`error_id: (err_[0-9a-f-]+)`)

func TestReportError_LogsOnceAndKeepsTheFirstId(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	known := mcpkit.ReportError(logger, "x", service.ErrNotFound)
	assert.Empty(t, known.ErrorID)
	assert.Empty(t, buf.String(), "an error the tools know is not logged")

	te := mcpkit.ReportError(logger, "failed", errors.New("cause"), "tool", "t")
	assert.Equal(t, mcpkit.CodeInternal, te.Code)
	require.NotEmpty(t, te.ErrorID)
	assert.Contains(t, te.Error(), te.ErrorID)
	assert.Contains(t, buf.String(), `"error_id":"`+te.ErrorID+`"`)
	assert.Contains(t, buf.String(), `"tool":"t"`)

	again := mcpkit.ReportError(logger, "failed", te)
	assert.Same(t, te, again, "an error that was reported is not logged a second time")
	assert.Equal(t, 1, strings.Count(buf.String(), "\n"))
	assert.NotPanics(t, func() { _ = mcpkit.ReportError(nil, "x", errors.New("no logger")) })
}
