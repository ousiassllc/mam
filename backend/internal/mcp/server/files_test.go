package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func postFile(t *testing.T, url, name string, data []byte, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(mcpkit.FileField, name)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func get(t *testing.T, url string, headers map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

func TestFiles_UploadAndDownloadThroughTicketsEndToEnd(t *testing.T) {
	f := newFixture(t)
	token := f.issue("agent")
	cs := f.mustConnect(token.Value)
	project, err := f.services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "スニーカーの PV"})
	require.NoError(t, err)

	var ticket mcpkit.UploadTicket
	res := callTool(t, cs, "create_upload_ticket", map[string]any{
		"purpose": "reference_image",
		"params":  map[string]string{"project_id": project.ID, "kind": "character", "name": "Mio", "tags": "hero"},
	}, &ticket)
	require.False(t, res.IsError, "%v", res.Content)
	assert.True(t, strings.HasPrefix(ticket.URL, f.http.URL+mcpkit.FilesPath), "the URL has the host the client used: %s", ticket.URL)
	assert.Equal(t, "POST", ticket.Method)

	// No bearer header is needed: the ticket is the credential. A wrong one is refused.
	status, _ := postFile(t, ticket.URL, "mio.png", pngBytes(t), map[string]string{"Authorization": "Bearer mam_mcp_wrong"})
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = postFile(t, ticket.URL, "notes.txt", []byte("not an image"), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, status, "service/upload.go validation is the screens' validation")
	status, body := postFile(t, ticket.URL, "mio.png", pngBytes(t), map[string]string{"Authorization": "Bearer " + token.Value})
	require.Equal(t, http.StatusCreated, status, "%v", body)
	result := body["result"].(map[string]any)
	assetID := result["asset_id"].(string)
	assert.NotEmpty(t, result["reference_id"])

	status, _ = postFile(t, ticket.URL, "mio.png", pngBytes(t), nil)
	assert.Equal(t, http.StatusNotFound, status, "an upload ticket works once")

	var link mcpkit.Link
	res = callTool(t, cs, "get_asset_download_url", map[string]any{"project_id": project.ID, "asset_id": assetID}, &link)
	require.False(t, res.IsError, "%v", res.Content)
	for range 2 { // a download ticket can be used again until it expires
		status, data := get(t, link.URL, nil)
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, pngBytes(t), data)
	}

	// The same asset id under another project is not found: no URL.
	res = callTool(t, cs, "get_asset_download_url", map[string]any{"project_id": "other", "asset_id": assetID}, nil)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content[0].(*mcp.TextContent).Text, "not_found:")
}

func TestFiles_TicketsStopWorkingWhenTheTokenIsRevoked(t *testing.T) {
	f := newFixture(t)
	token := f.issue("agent")
	cs := f.mustConnect(token.Value)
	project, err := f.services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "PV"})
	require.NoError(t, err)

	var ticket mcpkit.UploadTicket
	callTool(t, cs, "create_upload_ticket", map[string]any{"purpose": "reference_image", "params": map[string]string{"project_id": project.ID, "kind": "style", "name": "n"}}, &ticket)
	require.NoError(t, f.tokens.Revoke(context.Background(), token.Token.ID))

	status, _ := postFile(t, ticket.URL, "a.png", pngBytes(t), nil)
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestFiles_ForeignOriginIsRefused(t *testing.T) {
	f := newFixture(t)
	status, _ := get(t, f.http.URL+mcpkit.FilesPath+"anything", map[string]string{"Origin": "https://evil.example"})
	assert.Equal(t, http.StatusForbidden, status)
}
