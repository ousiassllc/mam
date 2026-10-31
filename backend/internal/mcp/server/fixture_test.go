package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/mcp/server"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	repo "github.com/ousiassllc/mam/backend/internal/repository/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

// fixture is a real MCP server over a temporary database, served by httptest (no listener of the Manager).
type fixture struct {
	t        *testing.T
	srv      *server.Server
	http     *httptest.Server
	tokens   *mcptoken.Service
	services *service.Services
	keys     *credentialtest.Memory
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testdb.Open(t)
	keys := credentialtest.New()
	services := servicetest.New(t, service.Deps{DB: db, Credentials: keys, AssetsDir: t.TempDir()})
	tokens := mcptoken.New(repo.New(db))
	srv := server.New(server.Options{
		Services: services, Tokens: tokens, JobTable: repository.NewJobRepository(db), JobHistory: jobhistory.New(db),
		Keys: keys, Version: "test", DataDir: t.TempDir(),
	})
	h := httptest.NewServer(srv.Handler)
	t.Cleanup(h.Close)
	return &fixture{t: t, srv: srv, http: h, tokens: tokens, services: services, keys: keys}
}

func (f *fixture) issue(name string) mcptoken.Issued {
	f.t.Helper()
	issued, err := f.tokens.Issue(context.Background(), name)
	require.NoError(f.t, err)
	return issued
}

func (f *fixture) url() string { return f.http.URL + server.Path }

// bearer adds an Authorization header to every request of a client.
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

// connect opens an MCP session with the token.
func (f *fixture) connect(token string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	return client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: f.url(), HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true,
	}, nil)
}

func (f *fixture) mustConnect(token string) *mcp.ClientSession {
	f.t.Helper()
	cs, err := f.connect(token)
	require.NoError(f.t, err)
	f.t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// rawPost sends one JSON-RPC initialize request with the headers.
func (f *fixture) rawPost(headers map[string]string) *http.Response {
	f.t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`
	req, err := http.NewRequest(http.MethodPost, f.url(), bytes.NewBufferString(body))
	require.NoError(f.t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(f.t, err)
	f.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	if out != nil && !res.IsError {
		raw, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, out))
	}
	return res
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	return buf.Bytes()
}

func serviceProjectInput(content string) service.CreateProjectInput {
	return service.CreateProjectInput{Content: content}
}
