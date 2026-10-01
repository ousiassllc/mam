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
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/media"
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
	services, cleanup, err := buildServices(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	gin.SetMode(gin.ReleaseMode)
	engine := router.New(services, router.Options{Logger: logger, Static: staticFS(cfg)})

	ln, err := listenLoopback(cfg.Port)
	if err != nil {
		return err
	}
	return runHTTP(ctx, engine, ln, logger, func(url string) {
		logger.Info("server started", "url", url, "data_dir", cfg.DataDir)
		if cfg.OpenBrowser {
			if err := openBrowser(url); err != nil {
				logger.Warn("could not open the browser; open the URL manually", "url", url, "err", err)
			}
		}
	})
}

// buildServices opens the database, builds every dependency of the services, recovers the work an
// abnormal termination left behind and starts the job queue. cleanup stops and closes all of it in
// the reverse order of construction (queue, services, Judge, database). It is nil on error, where
// buildServices has already released everything it had built.
func buildServices(ctx context.Context, cfg config.Config, logger *slog.Logger) (_ *service.Services, _ func(), err error) {
	var closers []func()
	release := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	defer func() {
		if err != nil {
			release()
		}
	}()

	// Leftovers of interrupted imports (tens of GB at most) go first, so the sweep below does not walk them.
	staleImports, err := service.RemoveStaleImports(cfg.AssetsDir())
	if err != nil {
		return nil, nil, fmt.Errorf("clean stale import directories: %w", err)
	}
	if len(staleImports) > 0 {
		logger.Warn("removed stale import directories", "count", len(staleImports))
	}

	// Leftovers of interrupted atomic writes must not be mistaken for finished assets.
	removed, err := fsutil.RemoveStaleTemp(cfg.AssetsDir())
	if err != nil {
		return nil, nil, fmt.Errorf("clean stale temp files: %w", err)
	}
	if len(removed) > 0 {
		logger.Warn("removed stale temp files", "count", len(removed))
	}

	db, err := repository.Open(ctx, cfg.DBPath(), logger)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	closers = append(closers, func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", "err", err)
		}
	})

	llmCfg, err := llm.LoadConfig(cfg.LLMConfigPath)
	if err != nil {
		return nil, nil, err
	}
	keys := credential.NewKeyring()
	reasoning := llm.NewClient(llmCfg, llm.NewBackends(llmCfg, keys, nil), llm.WithLogger(logger))

	modelCfg, err := modeladapter.LoadConfig(cfg.ModelConfigPath)
	if err != nil {
		return nil, nil, err
	}
	models := modeladapter.New(modelCfg, modeladapter.NewBackends(modelCfg, keys, nil, 0, modeladapter.WithStagingDir(filepath.Join(cfg.AssetsDir(), ".staging"))), modeladapter.WithLogger(logger))

	judging, err := newJudgeSetup(cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, judging.close)

	var previews *media.Tools // ffmpeg for the Take previews and Partial Take Use; nil when missing
	if tools, err := media.Look(); err != nil {
		logger.Warn("Take previews are limited: ffmpeg / ffprobe not found (4K Takes play as they are, Partial Take Use is off)")
	} else {
		previews = &tools
	}

	jevCfg, err := jev.LoadConfig(cfg.JevConfigPath)
	if err != nil {
		return nil, nil, err
	}
	// The Jev shadow run of the Repair Agent works once a TypeSafe API key is stored (credential provider "typesafe").
	shadow, err := jev.New(jevCfg, keys, nil)
	if err != nil {
		return nil, nil, err
	}

	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{Logger: logger})
	// Job handlers are registered by service.New (the plan job) and by the features that add more kinds.
	// The services are built before the queue starts: they subscribe to job changes.
	services := service.New(service.Deps{
		DB:          db,
		Jobs:        jobs,
		Credentials: keys,
		LLM:         reasoning,
		Models:      models,
		Logger:      logger,
		AssetsDir:   cfg.AssetsDir(),
		Frames:      judging.frames,
		Faces:       judging.engine(),
		Judge:       judging.config,
		Jev:         shadow,
		Media:       previews,
		Audio:       audioProviders(keys),
	})
	closers = append(closers, services.Close) // runs after jobs.Stop: no job change wakes the workers any more
	if err := recoverWork(ctx, services, jobs, logger); err != nil {
		return nil, nil, err
	}
	jobs.Start(ctx)
	closers = append(closers, jobs.Stop)
	return services, release, nil
}

// recoverWork cleans up after an abnormal termination: Shots left GENERATING are released before
// their jobs are failed.
func recoverWork(ctx context.Context, services *service.Services, jobs *jobqueue.Queue, logger *slog.Logger) error {
	released, err := services.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover shots: %w", err)
	}
	if released > 0 {
		logger.Warn("released shots interrupted while generating", "count", released)
	}
	resumed, failed, err := jobs.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	}
	if resumed+failed > 0 {
		logger.Warn("recovered unfinished jobs after abnormal termination", "resumed", resumed, "failed", failed)
	}
	return nil
}

// runHTTP serves handler on ln until ctx is cancelled, then shuts the server down gracefully
// (in-flight requests get shutdownTimeout to finish). onReady, when not nil, is called with the
// base URL once the server is accepting connections.
func runHTTP(ctx context.Context, handler http.Handler, ln net.Listener, logger *slog.Logger, onReady func(url string)) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute, // the largest upload (a video bundle) must arrive within it
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

// audioProviders are the vendors of the Audio Director: ElevenLabs (TTS, cloned voices, Music,
// Sound Effects; API key "elevenlabs") and Sync Labs (Lip Sync; API key "synclabs").
func audioProviders(keys credential.Store) audiodirector.Providers {
	p := audiodirector.ElevenLabsProviders(audiodirector.NewElevenLabs(audiodirector.Config{}, keys, nil))
	p.LipSync = audiodirector.NewSyncLabs(audiodirector.SyncLabsConfig{}, keys, nil)
	return p
}
