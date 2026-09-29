// Command server starts the local mam server on 127.0.0.1 and opens the default browser.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/static"
)

const shutdownTimeout = 10 * time.Second

// staticFS serves assets from disk when --static-dir is set (make dev), else from the binary.
func staticFS(cfg config.Config) fs.FS {
	if cfg.StaticDir != "" {
		return os.DirFS(cfg.StaticDir)
	}
	return static.Dist()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "mam:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(args, getenv, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	logger, closeLog, err := logging.New(cfg.LogDir(), cfg.LogLevel, stderr)
	if err != nil {
		return err
	}
	defer func() { _ = closeLog.Close() }()

	if err := serve(ctx, cfg, logger); err != nil {
		logger.Error("server terminated", "err", err)
		return err
	}
	return nil
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	// Leftovers of interrupted atomic writes must not be mistaken for finished assets.
	removed, err := fsutil.RemoveStaleTemp(cfg.AssetsDir())
	if err != nil {
		return fmt.Errorf("clean stale temp files: %w", err)
	}
	if len(removed) > 0 {
		logger.Warn("removed stale temp files", "count", len(removed))
	}

	db, err := repository.Open(ctx, cfg.DBPath(), logger)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", "err", err)
		}
	}()

	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{Logger: logger})
	// Feature Issues register their job handlers here: jobs.Register(domain.JobGenerate, h, resumable).
	resumed, failed, err := jobs.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	}
	if resumed+failed > 0 {
		logger.Warn("recovered unfinished jobs after abnormal termination", "resumed", resumed, "failed", failed)
	}
	jobs.Start(ctx)
	defer jobs.Stop()

	services := service.New(service.Deps{
		DB:          db,
		Jobs:        jobs,
		Credentials: credential.NewKeyring(),
		Logger:      logger,
	})

	gin.SetMode(gin.ReleaseMode)
	engine := router.New(services, router.Options{Logger: logger, Static: staticFS(cfg)})

	ln, err := listenLoopback(cfg.Port)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	url := baseURL(ln)
	logger.Info("server started", "url", url, "data_dir", cfg.DataDir)
	if cfg.OpenBrowser {
		if err := openBrowser(url); err != nil {
			logger.Warn("could not open the browser; open the URL manually", "url", url, "err", err)
		}
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
