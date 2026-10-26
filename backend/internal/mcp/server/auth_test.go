package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpoint_RefusesRequestsWithoutAValidToken(t *testing.T) {
	f := newFixture(t)
	revoked := f.issue("revoked")
	require.NoError(t, f.tokens.Revoke(context.Background(), revoked.Token.ID))

	for name, header := range map[string]string{
		"no header":      "",
		"not bearer":     "Basic dXNlcjpwYXNz",
		"empty bearer":   "Bearer ",
		"unknown value":  "Bearer mam_mcp_" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"revoked token":  "Bearer " + revoked.Value,
		"not our format": "Bearer sk-1234",
	} {
		headers := map[string]string{}
		if header != "" {
			headers["Authorization"] = header
		}
		resp := f.rawPost(headers)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, name)
		assert.Contains(t, resp.Header.Get("WWW-Authenticate"), "Bearer", name)
		assert.Empty(t, resp.Header.Get("Mcp-Session-Id"), "%s: no MCP session may start", name)
	}
	_, err := f.connect(revoked.Value)
	assert.Error(t, err, "the client cannot initialize with a revoked token")
}

func TestEndpoint_ServesAnAgentWithAValidToken(t *testing.T) {
	f := newFixture(t)
	cs := f.mustConnect(f.issue("agent").Value)

	tools, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = tool
	}
	for _, want := range []string{"list_projects", "get_job", "list_credentials", "set_credential", "delete_credential", "create_upload_ticket", "get_server_info"} {
		assert.Contains(t, names, want)
	}
	assert.True(t, names["list_projects"].Annotations.ReadOnlyHint)
	assert.True(t, *names["delete_credential"].Annotations.DestructiveHint)
	assert.False(t, *names["set_credential"].Annotations.DestructiveHint)

	var info struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		ListenAddr string `json:"listen_addr"`
		ToolCount  int    `json:"tool_count"`
	}
	res := callTool(t, cs, "get_server_info", map[string]any{}, &info)
	require.False(t, res.IsError)
	assert.Equal(t, "mam", info.Name)
	assert.Equal(t, "test", info.Version)
	assert.Equal(t, len(tools.Tools), info.ToolCount)

	_, err = f.services.Projects.Create(context.Background(), serviceProjectInput("春の新作スニーカーのPV"))
	require.NoError(t, err)
	var list struct {
		Projects []struct{ ID, Name, State string } `json:"projects"`
	}
	res = callTool(t, cs, "list_projects", map[string]any{}, &list)
	require.False(t, res.IsError, "%v", res.Content)
	require.Len(t, list.Projects, 1)
	assert.Equal(t, "DRAFT", list.Projects[0].State)
}

func TestEndpoint_RevokingATokenCutsTheSessionAtOnce(t *testing.T) {
	f := newFixture(t)
	issued := f.issue("agent")
	cs := f.mustConnect(issued.Value)
	callTool(t, cs, "get_server_info", map[string]any{}, nil)

	require.NoError(t, f.tokens.Revoke(context.Background(), issued.Token.ID))
	_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_server_info", Arguments: map[string]any{}})
	assert.Error(t, err, "the open session must not outlive its token")
}

func TestEndpoint_ASessionBelongsToTheTokenThatOpenedIt(t *testing.T) {
	f := newFixture(t)
	a, b := f.issue("a"), f.issue("b")
	cs := f.mustConnect(a.Value)

	// Another valid token presenting a's session id is refused.
	req, err := http.NewRequest(http.MethodPost, f.url(), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+b.Value)
	req.Header.Set("Mcp-Session-Id", cs.ID())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestEndpoint_RefusesAForeignOrigin(t *testing.T) {
	f := newFixture(t)
	token := f.issue("agent").Value
	host := f.http.Listener.Addr().String()

	for origin, want := range map[string]int{
		"":                              http.StatusOK, // a native client sends none
		"http://" + host:                http.StatusOK, // a page served from the same host
		"https://evil.example":          http.StatusForbidden,
		"http://evil.example:" + "8788": http.StatusForbidden,
		"null":                          http.StatusForbidden,
		"::not a url::":                 http.StatusForbidden,
	} {
		headers := map[string]string{"Authorization": "Bearer " + token}
		if origin != "" {
			headers["Origin"] = origin
		}
		resp := f.rawPost(headers)
		assert.Equal(t, want, resp.StatusCode, "Origin %q", origin)
	}
	// The check comes before the token: a foreign page learns nothing about tokens.
	resp := f.rawPost(map[string]string{"Origin": "https://evil.example"})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestEndpoint_OnlyMCPAndFilesPathsExist(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/", "/mcp/", "/settings", "/mcp-files"} {
		resp, err := http.Get(f.http.URL + path)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, path)
	}
}
