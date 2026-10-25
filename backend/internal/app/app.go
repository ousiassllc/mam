// Package app is the startup of mam, shared by every front: it loads the settings, locks the data
// directory, opens the logger, wires the services (the DI root), recovers the work an abnormal
// termination left behind, serves the router on 127.0.0.1 and shuts down gracefully. The fronts
// (cmd/server opens the default browser, cmd/wails loads the URL in a native window) differ only in
// what they do with the URL that Options.OnReady receives.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/version"
	"github.com/ousiassllc/mam/static"
)

// Options is what a front passes to Run.
type Options struct {
	// Args are the command-line arguments without the program name (config.Load).
	Args []string
	// Getenv reads the MAM_* environment variables; nil means os.Getenv.
	Getenv func(string) string
	// Stderr receives the usage text of --help, the flag errors and a copy of the log. Nil discards it.
	Stderr io.Writer
	// OnReady, when not nil, is called with the base URL (http://127.0.0.1:<port>) once the server
	// accepts connections. It runs on the goroutine of Run, so it must not block.
	OnReady func(url string)
	// Update, when not nil, turns the automatic update on (the Wails front only).
	Update *UpdateOptions
	// OpenBrowser opens a URL in the default browser of this machine (never in the Wails window): the browser login
	// of "Sign in with ChatGPT" uses it. Nil: the login screen shows the URL to open by hand.
	OpenBrowser func(url string) error
}

// staticFS serves assets from disk when --static-dir is set (make dev), else from the binary.
func staticFS(cfg config.Config) fs.FS {
	if cfg.StaticDir != "" {
		return os.DirFS(cfg.StaticDir)
	}
	return static.Dist()
}

// Run starts the server and blocks until ctx is cancelled (graceful shutdown, nil) or the server
// fails. --help prints the usage to Options.Stderr and returns nil without starting anything.
func Run(ctx context.Context, opts Options) error {
	getenv, stderr := opts.Getenv, opts.Stderr
	if getenv == nil {
		getenv = os.Getenv
	}
	if stderr == nil {
		stderr = io.Discard
	}

	cfg, err := config.Load(opts.Args, getenv, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	// One instance per data directory: a second one would delete the temporary files and import
	// directories the running instance is writing (startup cleanup) and race it on the database.
	lock, err := fsutil.LockDir(cfg.DataDir)
	if errors.Is(err, fsutil.ErrDirLocked) {
		return fmt.Errorf("another mam is already using the data directory %s; stop it first or choose another with --data-dir", cfg.DataDir)
	}
	if err != nil {
		return err
	}
	defer lock.Unlock()

	logger, closeLog, err := logging.New(cfg.LogDir(), cfg.LogLevel, stderr)
	if err != nil {
		return err
	}
	defer func() { _ = closeLog.Close() }()

	if err := serve(ctx, cfg, logger, opts); err != nil {
		logger.Error("server terminated", "err", err)
		return err
	}
	return nil
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, opts Options) error {
	gpt := chatgpt.New(chatgpt.Options{
		Keys: credential.NewKeyring(), DataDir: cfg.DataDir, Open: opts.OpenBrowser, Logger: logger,
		CodexLayout: codexLayout(logger), Version: version.Version,
	})
	defer gpt.Close() // the Codex app-server is a child process of mam
	services, db, cleanup, err := buildServices(ctx, cfg, logger, gpt)
	if err != nil {
		return err
	}
	defer cleanup()

	gin.SetMode(gin.ReleaseMode)
	llmFile, llmPinned := cfg.LLMSettingsFile()
	modelFile, modelPinned := cfg.ModelSettingsFile()
	settings := settingsroutes.New(llmFile, llmPinned, modelFile, modelPinned)
	settings.LocalModels = services.LocalModels
	settings.Keys = credential.NewKeyring() // the OS secure storage: the connection test of an API key reads a saved key from it
	settings.Workflows = services.Workflows
	// A saved image / video assignment applies at once, as a saved endpoint or model does.
	settings.Models.SetOnChange(func() {
		if err := services.LocalModels.Reload(); err != nil {
			logger.Error("apply the saved model settings failed", "err", err)
		}
	})
	settings.ChatGPT = gpt
	settings.ModelManager = services.ModelManager
	settings.Gentest = services.Gentest
	if opts.Update != nil {
		var stop func()
		settings.Update, stop = startUpdates(ctx, opts.Update, services, repository.NewJobRepository(db), logger)
		defer stop() // before cleanup: the check in flight is cancelled while the services are still there
	}
	var stopMCP func()
	settings.MCP, stopMCP = startMCP(cfg, services, settings, db, logger)
	defer stopMCP() // before cleanup: tool calls in flight use the services
	engine := router.New(services, router.Options{Logger: logger, Static: staticFS(cfg), Settings: settings})

	ln, err := listenLoopback(cfg.Port)
	if err != nil {
		return err
	}
	return runHTTP(ctx, engine, ln, logger, func(url string) {
		logger.Info("server started", "url", url, "data_dir", cfg.DataDir)
		if opts.OnReady != nil {
			opts.OnReady(url)
		}
	})
}
