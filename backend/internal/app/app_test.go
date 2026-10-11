package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

func TestListenLoopback_BindsOnlyLoopbackAndFallsBackWhenBusy(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = busy.Close() }()
	busyPort := busy.Addr().(*net.TCPAddr).Port

	ln, err := listenLoopback(busyPort)
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	addr := ln.Addr().(*net.TCPAddr)
	assert.True(t, addr.IP.IsLoopback(), "must bind loopback only, got %s", addr.IP)
	assert.NotEqual(t, busyPort, addr.Port, "a busy preferred port must fall back to another one")
	assert.Equal(t, "http://"+addr.String(), baseURL(ln))
}

func TestListenLoopback_UsesPreferredPortWhenFree(t *testing.T) {
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.Addr().(*net.TCPAddr).Port
	require.NoError(t, probe.Close())

	ln, err := listenLoopback(port)
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	assert.Equal(t, port, ln.Addr().(*net.TCPAddr).Port)
}

// Run reports the URL it listens on through OnReady, after the server accepts connections; --help
// prints the usage and starts nothing.
func TestRun_OnReadyReceivesTheListeningURL(t *testing.T) {
	keyring.MockInit()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Args:    []string{"-data-dir", t.TempDir(), "-port", "0"},
			Getenv:  func(string) string { return "" },
			OnReady: func(url string) { ready <- url },
		})
	}()

	var url string
	select {
	case url = <-ready:
	case <-time.After(15 * time.Second):
		t.Fatal("OnReady was never called")
	}
	assert.Regexp(t, `^http://127\.0\.0\.1:\d+$`, url)
	resp, err := http.Get(url + "/healthz")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestRun_HelpPrintsUsageAndStartsNothing(t *testing.T) {
	var out syncBuffer
	called := false
	err := Run(context.Background(), Options{
		Args:    []string{"-help"},
		Getenv:  func(string) string { return "" },
		Stderr:  &out,
		OnReady: func(string) { called = true },
	})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "-data-dir")
	assert.False(t, called)
}

// TestRun_StartsServerServesRequestsAndShutsDownCleanly boots the real wiring on a
// temp data dir: migrations, job recovery, router, then graceful shutdown.
func TestRun_StartsServerServesRequestsAndShutsDownCleanly(t *testing.T) {
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

	resp, err := http.Get(url + "/healthz")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"ok":true,"database":"ok"}`, string(body))

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down")
	}
	assert.FileExists(t, filepath.Join(dataDir, "mam.db"))
	assert.FileExists(t, filepath.Join(dataDir, "logs", "mam.log"))
}

func startedURL(log string) string {
	for _, line := range strings.Split(log, "\n") {
		if !strings.Contains(line, "server started") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if v, ok := strings.CutPrefix(f, "url="); ok {
				if _, err := strconv.Atoi(v[strings.LastIndex(v, ":")+1:]); err == nil {
					return v
				}
			}
		}
	}
	return ""
}

func TestRun_RefusesASecondInstanceOnTheSameDataDirAndLeavesTheFirstOnesWorkAlone(t *testing.T) {
	keyring.MockInit()
	dataDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Args: []string{"-data-dir", dataDir, "-port", "0", "-no-browser"}, Getenv: func(string) string { return "" }, Stderr: &out})
	}()
	require.Eventually(t, func() bool { return startedURL(out.String()) != "" }, 15*time.Second, 20*time.Millisecond, "log: %s", out.String())

	// A staging file of the running instance: the startup cleanup of a second one would delete it.
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "assets"), 0o750))
	staging, err := fsutil.CreateStagingTemp(filepath.Join(dataDir, "assets"))
	require.NoError(t, err)
	defer func() { _ = staging.Close() }()

	// Bounded, so that a second instance that wrongly starts ends the run instead of serving forever.
	secondCtx, stopSecond := context.WithTimeout(ctx, 3*time.Second)
	defer stopSecond()
	var second syncBuffer
	err = Run(secondCtx, Options{Args: []string{"-data-dir", dataDir, "-port", "0", "-no-browser"}, Getenv: func(string) string { return "" }, Stderr: &second})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already using the data directory")
	assert.FileExists(t, staging.Name(), "the running instance's work was not cleaned up")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down")
	}
	lock, err := fsutil.LockDir(dataDir)
	require.NoError(t, err, "the directory is free again once the first instance stopped")
	lock.Unlock()
}

func TestValidateArgs(t *testing.T) {
	getenv := func(string) string { return "" }
	require.NoError(t, ValidateArgs([]string{"-data-dir", t.TempDir(), "-no-browser"}, getenv))

	var usage *UsageError
	err := ValidateArgs([]string{"-help"}, getenv)
	require.ErrorAs(t, err, &usage)
	assert.True(t, usage.Help)
	assert.Contains(t, usage.Text, "-data-dir")

	err = ValidateArgs([]string{"-no-such-flag"}, getenv)
	require.ErrorAs(t, err, &usage)
	assert.False(t, usage.Help)
	assert.Contains(t, usage.Text, "no-such-flag")
	assert.Contains(t, usage.Text, "-data-dir")

	err = ValidateArgs([]string{"-port", "70000"}, getenv)
	require.ErrorAs(t, err, &usage)
	assert.False(t, usage.Help)
	assert.Contains(t, usage.Text, "invalid port 70000")
}
