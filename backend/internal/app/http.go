package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Variables so tests can shorten them.
var (
	// shutdownTimeout is how long in-flight requests get to finish after ctx is cancelled.
	shutdownTimeout = 10 * time.Second
	// abortTimeout is how long runHTTP waits for handlers to return after their request contexts
	// have been cancelled because the graceful shutdown ran out of time.
	abortTimeout = 5 * time.Second
)

// runHTTP serves handler on ln until ctx is cancelled, then shuts the server down gracefully
// (in-flight requests get shutdownTimeout to finish). Requests still running after that are
// aborted: their contexts are cancelled, the connections are closed and runHTTP waits (up to
// abortTimeout) for the handlers to return, so the caller can close what they use. onReady, when
// not nil, is called with the base URL once the server is accepting connections.
func runHTTP(ctx context.Context, handler http.Handler, ln net.Listener, logger *slog.Logger, onReady func(url string)) error {
	// Request contexts must outlive ctx so that the graceful shutdown can let requests finish;
	// they are cancelled only when the graceful shutdown times out.
	reqCtx, abortRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer abortRequests()
	var inFlight sync.WaitGroup
	tracked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inFlight.Add(1)
		defer inFlight.Done()
		handler.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Handler:           tracked,
		BaseContext:       func(net.Listener) context.Context { return reqCtx },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute, // the default for a request body; uploads are held to an idle time instead (web/handler/multipart.go)
		IdleTimeout:       2 * time.Minute,
		// No WriteTimeout: media is streamed and can take arbitrarily long.
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	if onReady != nil {
		onReady(baseURL(ln))
	}

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := srv.Shutdown(shutdownCtx)
	if err == nil {
		return nil
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("shutdown: %w", err)
	}

	// Graceful shutdown timed out: abort what is left instead of leaving handlers running
	// against services that are about to be closed. This is the requested stop, not a failure.
	logger.Warn("shutdown timed out; aborting in-flight requests", "timeout", shutdownTimeout)
	abortRequests()
	if err := srv.Close(); err != nil {
		logger.Warn("closing server", "err", err)
	}
	drained := make(chan struct{})
	go func() { inFlight.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(abortTimeout):
		logger.Warn("in-flight requests did not stop after being aborted", "timeout", abortTimeout)
	}
	return nil
}
