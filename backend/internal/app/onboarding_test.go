package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/service/onboarding"
)

// The production wiring of the first-run guide: the flag is a file of the data directory (onboarding.Service over
// DataDir/onboarding.json), and a fresh data directory is a first run.
func TestRun_AFreshDataDirSendsTheRootToTheGuideAndAFinishedOneDoesNot(t *testing.T) {
	keyring.MockInit()
	dataDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Args: []string{"-data-dir", dataDir, "-port", "0", "-no-browser"}, Getenv: func(string) string { return "" }, Stderr: &out})
	}()
	var url string
	require.Eventually(t, func() bool {
		url = startedURL(out.String())
		return url != ""
	}, 15*time.Second, 20*time.Millisecond, "server never reported its URL; log: %s", out.String())
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	first, err := client.Get(url + "/")
	require.NoError(t, err)
	_ = first.Body.Close()
	assert.Equal(t, http.StatusFound, first.StatusCode)
	assert.Equal(t, "/onboarding", first.Header.Get("Location"))

	require.NoError(t, os.WriteFile(filepath.Join(dataDir, onboarding.FileName), []byte(`{"completed": true}`), 0o600))
	second, err := client.Get(url + "/")
	require.NoError(t, err)
	_ = second.Body.Close()
	assert.Equal(t, http.StatusOK, second.StatusCode, "once the flag file says the guide is done, / is the project list")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down")
	}
}
