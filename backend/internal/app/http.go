package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

// runHTTP serves handler on ln until ctx is cancelled, then shuts the server down gracefully
// (in-flight requests get shutdownTimeout to finish). onReady, when not nil, is called with the
// base URL once the server is accepting connections.
func runHTTP(ctx context.Context, handler http.Handler, ln net.Listener, logger *slog.Logger, onReady func(url string)) error {
	srv := &http.Server{
		Handler:           handler,
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
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
