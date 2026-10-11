package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cancelling ctx shuts the server down gracefully: a request in flight still gets its answer,
// runHTTP returns nil only after it, and the port is closed afterwards.
func TestRunHTTP_ContextCancelShutsDownGracefullyAfterInFlightRequests(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "done")
	})

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	result := make(chan error, 1)
	go func() {
		result <- runHTTP(ctx, handler, ln, slog.New(slog.DiscardHandler), func(url string) { ready <- url })
	}()
	url := <-ready
	assert.Equal(t, "http://"+ln.Addr().String(), url)

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil {
			body <- "error: " + err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()
	<-entered
	cancel()

	select {
	case err := <-result:
		t.Fatalf("runHTTP returned %v while a request was still in flight", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	assert.Equal(t, "done", <-body)
	require.NoError(t, <-result)

	_, err = net.DialTimeout("tcp4", ln.Addr().String(), time.Second)
	assert.Error(t, err, "the listener is closed after shutdown")
}

// A request that outlives the graceful shutdown has its context cancelled and runHTTP returns
// (without an error) only after the handler is done, so the caller can close what it uses.
func TestRunHTTP_AbortsRequestsStillRunningAfterShutdownTimeout(t *testing.T) {
	origTimeout := shutdownTimeout
	shutdownTimeout = 100 * time.Millisecond
	t.Cleanup(func() { shutdownTimeout = origTimeout })

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	entered := make(chan struct{})
	var handlerDone atomic.Bool
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		time.Sleep(50 * time.Millisecond) // cleanup after the abort
		handlerDone.Store(true)
	})

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	result := make(chan error, 1)
	go func() {
		result <- runHTTP(ctx, handler, ln, slog.New(slog.DiscardHandler), func(url string) { ready <- url })
	}()
	url := <-ready

	go func() {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered
	cancel()

	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("runHTTP did not return after the shutdown timeout")
	}
	assert.True(t, handlerDone.Load(), "runHTTP returned while the handler was still running")
}

func TestRunHTTP_AListenerFailureIsReturnedAsAServeError(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close()) // Serve fails at once

	err = runHTTP(context.Background(), http.NotFoundHandler(), ln, slog.New(slog.DiscardHandler), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "serve:")
}
