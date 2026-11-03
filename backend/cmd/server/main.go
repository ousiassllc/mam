// Command server starts the local mam server on 127.0.0.1 and opens the default browser.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ousiassllc/mam/backend/internal/app"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

func main() {
	// The guard that ends a child of mam when mam is killed (macOS) is this binary started again: nothing else may start first.
	if proc.RunGuardIfRequested() {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "mam:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	// --detect-gpu / --detect-comfyui print a report and exit; nothing else starts.
	if handled, err := app.RunDetect(ctx, args, os.Stdout); handled {
		return err
	}
	// app.Run parses the same arguments again; the parse here only decides whether to open the
	// browser, and prints --help / a flag error exactly once before app.Run would.
	cfg, err := config.Load(args, getenv, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	return app.Run(ctx, app.Options{
		Args:        args,
		Getenv:      getenv,
		Stderr:      stderr,
		OpenBrowser: openBrowser,
		OnReady: func(url string) {
			if !cfg.OpenBrowser {
				return
			}
			if err := openBrowser(url); err != nil {
				slog.Warn("could not open the browser; open the URL manually", "url", url, "err", err)
			}
		},
	})
}
