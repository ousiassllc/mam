package desktop

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/app"
)

func noEnv(string) string { return "" }

// Stop shuts the server down gracefully and returns only once the port is closed and the data
// directory is free again; a stop is not a failure.
func TestServer_StopShutsDownGracefullyAndIsNotAFailure(t *testing.T) {
	keyring.MockInit()
	ready := make(chan string, 1)
	failures := make(chan error, 1)
	s := StartServer(app.Options{
		Args:    []string{"-data-dir", t.TempDir(), "-port", "0"},
		Getenv:  noEnv,
		OnReady: func(url string) { ready <- url },
	}, func(err error) { failures <- err })

	var url string
	select {
	case url = <-ready:
	case <-time.After(15 * time.Second):
		t.Fatal("the server never became ready")
	}
	resp, err := http.Get(url + "/healthz")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	require.NoError(t, s.Stop())
	require.NoError(t, s.Stop(), "Stop can be called again")
	_, err = http.Get(url + "/healthz")
	assert.Error(t, err, "the port is closed once Stop returned")
	assert.Empty(t, failures)
}

// A startup error (here: a data directory that is already in use) reaches onFailure on its own.
func TestServer_ReportsAStartupFailure(t *testing.T) {
	keyring.MockInit()
	dataDir := t.TempDir()
	first := make(chan string, 1)
	s1 := StartServer(app.Options{
		Args:    []string{"-data-dir", dataDir, "-port", "0"},
		Getenv:  noEnv,
		OnReady: func(url string) { first <- url },
	}, func(err error) { t.Errorf("first server failed: %v", err) })
	select {
	case <-first:
	case <-time.After(15 * time.Second):
		t.Fatal("the first server never became ready")
	}
	defer func() { require.NoError(t, s1.Stop()) }()

	failures := make(chan error, 1)
	s2 := StartServer(app.Options{Args: []string{"-data-dir", dataDir, "-port", "0"}, Getenv: noEnv}, func(err error) { failures <- err })
	select {
	case err := <-failures:
		assert.ErrorContains(t, err, "already using the data directory")
	case <-time.After(15 * time.Second):
		t.Fatal("no failure was reported")
	}
	assert.Error(t, s2.Stop())
}
