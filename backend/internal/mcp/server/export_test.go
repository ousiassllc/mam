package server_test

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// #932: the ZIP of generation results is reachable only through a tool call that carries a valid token, and its URL stops working
// when that token is revoked. Nobody else can fetch results: not without a token, not by guessing the URL.
func TestExportGenerationResults_NeedsAValidTokenAndItsUrlDiesWithTheToken(t *testing.T) {
	f := newFixture(t)
	rig := gentesttest.New(t)
	f.services.Gentest = rig.Svc
	ref, err := rig.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(t, err)
	require.Equal(t, 1, rig.Wait(ref.BatchID).Succeeded)

	_, err = f.connect("")
	assert.Error(t, err, "no token: no session, so no tool")
	_, err = f.connect("mam_mcp_wrong")
	assert.Error(t, err, "a wrong token: no session")

	token := f.issue("agent")
	cs := f.mustConnect(token.Value)
	var out mcpgentest.ExportOutput
	res := callTool(t, cs, "export_generation_results", map[string]any{"latest": 5}, &out)
	require.False(t, res.IsError, "%v", res.Content)
	require.Len(t, out.Files, 1)
	url := out.Files[0].URL
	assert.True(t, strings.HasPrefix(url, f.http.URL+mcpkit.FilesPath), "the ticket URL of the file endpoint of this server: %s", url)
	assert.NotContains(t, url, out.Items[0].ResultID, "the URL carries a random ticket, not the result ids")

	status, body := get(t, url, nil)
	require.Equal(t, http.StatusOK, status)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	assert.Len(t, zr.File, 2, "prompts/prompts.csv and the photo")

	status, _ = get(t, f.http.URL+mcpkit.FilesPath+"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", nil)
	assert.Equal(t, http.StatusNotFound, status, "a guess is a 404")
	status, _ = get(t, url, map[string]string{"Origin": "https://evil.example"})
	assert.Equal(t, http.StatusForbidden, status, "a page of another origin cannot fetch it")

	require.NoError(t, f.tokens.Revoke(context.Background(), token.Token.ID))
	status, body = get(t, url, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "the token that issued the URL is gone")
	assert.NotContains(t, string(body), "PK")
}
