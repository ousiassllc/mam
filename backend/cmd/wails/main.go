//go:build windows || darwin

// Command wails is the desktop front of mam (Windows: WebView2, macOS on Apple Silicon: WKWebView): a Wails v3
// window that opens the same 127.0.0.1 server as the browser front (cmd/server). The window is a shell for that
// URL: the Wails asset server, bindings and events are not used (spec/architecture/desktop.md). What differs per
// OS (error dialog, the navigation event, the update options) is in dialog_*.go and platform_*.go.
package main

import (
	"context"
	"errors"
	"os"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/ousiassllc/mam/backend/internal/app"
	"github.com/ousiassllc/mam/backend/internal/desktop"
)

const (
	appName = "MAM"
	// uniqueID identifies the single running instance (Wails SingleInstance).
	uniqueID = "com.ousiassllc.mam"
	// waitingPage is shown until the server is ready: only the background colour of the app layout
	// (bg-neutral-950), so there is no white flash before the server answers.
	waitingPage = `<!doctype html><html><body style="margin:0;background:#0a0a0a"></body></html>`
)

func main() {
	// --detect-gpu / --detect-comfyui (the installer calls them) print a report and exit: no window, no
	// single-instance check, no data directory.
	if handled, err := app.RunDetect(context.Background(), os.Args[1:], os.Stdout); handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(run(os.Args[1:], os.Getenv))
}

func run(args []string, getenv func(string) string) int {
	var window atomic.Pointer[application.WebviewWindow]
	var server atomic.Pointer[desktop.Server]

	// The automatic update (spec/architecture/desktop.md §Auto update): the update service hands a verified
	// installer to updater, which quits the application; OnShutdown runs the installer (Windows) or the dmg helper
	// (macOS) once the server stopped.
	var wails *application.App
	updater := desktop.NewUpdater(func() { wails.Quit() })

	options := application.Options{
		Name:        appName,
		Description: "Multi-agent AI video production studio",
		// A second launch restores and raises the first window, then exits (inside application.New,
		// before the data directory is touched). Its arguments are dropped.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: uniqueID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if w := window.Load(); w != nil {
					raise(w)
				}
			},
		},
		// The last window closed, or an update asks to quit: stop the server gracefully before the process ends,
		// then (only for an update) run the installer, which restarts the application after it installed.
		OnShutdown: func() {
			if s := server.Load(); s != nil {
				_ = s.Stop() // a failed shutdown is already in the log file
			}
			if err := updater.RunPending(); err != nil {
				showError("アップデートのインストーラーを起動できませんでした。アプリを起動し直してください。\n" + err.Error())
			}
		},
	}
	configure(&options)
	wails = application.New(options)

	// Startup errors have no console (-H windowsgui on Windows, a GUI app on macOS): they go to a native dialog.
	if err := app.ValidateArgs(args, getenv); err != nil {
		showError(errorText(err))
		return 1
	}

	w := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            appName,
		Width:            1440,
		Height:           900,
		MinWidth:         1024,
		MinHeight:        700,
		HTML:             waitingPage,
		BackgroundColour: application.NewRGB(10, 10, 10),
	})
	window.Store(w)

	nav := desktop.NewNavigator(func(url string) { w.SetURL(url) })
	w.OnWindowEvent(navigationCompleted, func(*application.WindowEvent) { nav.WindowReady() })

	server.Store(desktop.StartServer(app.Options{
		Args:    args,
		Getenv:  getenv,
		OnReady: nav.SetURL,
		Update:  updateOptions(updater),
		// The ChatGPT login opens the default browser, not the webview: the login page must not run inside the app window.
		OpenBrowser: func(url string) error { return wails.Browser.OpenURL(url) },
	}, func(err error) {
		// The server is already down: no graceful shutdown to wait for.
		showError(errorText(err))
		os.Exit(1)
	}))

	if err := wails.Run(); err != nil {
		showError(err.Error())
		return 1
	}
	return 0
}

// raise brings the window back from the taskbar (the Dock on macOS) and to the front.
func raise(w *application.WebviewWindow) {
	w.Restore()
	w.Show()
	w.Focus()
}

// errorText is what the user reads: --help and a bad command line show the usage, a startup error
// its message.
func errorText(err error) string {
	var usage *app.UsageError
	if errors.As(err, &usage) {
		return usage.Text
	}
	return err.Error()
}
