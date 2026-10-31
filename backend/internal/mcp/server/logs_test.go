package server_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An agent reads the app log through the real server (token, session, schema validation), and neither the
// log nor what the tool returns holds the secrets the session handled: its own token, a rejected token and
// the API key it stored.
func TestEndpoint_GetAppLogsReadsTheLogWithoutSecrets(t *testing.T) {
	f := newFixture(t)
	issued := f.issue("agent")
	const (
		rejected = "mam_mcp_FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE0000"
		apiKey   = "FAKEAPIKEY-0123456789-FAKE"
	)
	assert.Equal(t, 401, f.rawPost(map[string]string{"Authorization": "Bearer " + rejected}).StatusCode)
	cs := f.mustConnect(issued.Value)
	require.False(t, callTool(t, cs, "set_credential", map[string]any{"provider": "kling", "api_key": apiKey}, nil).IsError)
	require.True(t, callTool(t, cs, "set_credential", map[string]any{"provider": "unknown-vendor", "api_key": apiKey}, nil).IsError)

	// Nothing the session did wrote a secret to the log (the file may not exist: the server logs at start and on failure only).
	file, err := os.ReadFile(filepath.Join(f.logDir, "mam.log"))
	if !os.IsNotExist(err) {
		require.NoError(t, err)
	}
	for _, secret := range []string{issued.Value, rejected, apiKey} {
		assert.NotContains(t, string(file), secret, "the log file")
	}

	// A line that does carry secrets (a mistake in some future code) is still not returned.
	f.logger.Error("upstream refused "+apiKey, "authorization", "Bearer "+issued.Value, "err", "token="+rejected)
	var out struct {
		Entries []map[string]any `json:"entries"`
	}
	res := callTool(t, cs, "get_app_logs", map[string]any{"level": "error", "limit": 500}, &out)
	require.False(t, res.IsError)
	require.Len(t, out.Entries, 1)
	raw, err := json.Marshal(res)
	require.NoError(t, err)
	for _, secret := range []string{issued.Value, rejected, apiKey} {
		assert.NotContains(t, string(raw), secret, "the tool's answer")
	}
}
