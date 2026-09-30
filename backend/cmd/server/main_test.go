package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
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

func TestBrowserCommand_PerOS(t *testing.T) {
	const url = "http://127.0.0.1:8787"
	for goos, wantName := range map[string]string{"windows": "rundll32", "darwin": "open", "linux": "xdg-open"} {
		cmd, err := browserCommand(goos, url)
		require.NoError(t, err, goos)
		assert.Equal(t, url, cmd.Args[len(cmd.Args)-1])
		assert.Equal(t, wantName, filepath.Base(cmd.Args[0]), goos)
	}
	_, err := browserCommand("plan9", url)
	assert.Error(t, err)
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
		done <- run(ctx, []string{"-data-dir", dataDir, "-port", "0", "-no-browser"}, func(string) string { return "" }, &out)
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
