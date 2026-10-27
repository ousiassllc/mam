//go:build darwin

package main

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/ousiassllc/mam/backend/internal/app"
	"github.com/ousiassllc/mam/backend/internal/desktop"
)

// navigationCompleted is the window event of a finished navigation (the first one is the waiting page).
var navigationCompleted = events.Mac.WebViewDidFinishNavigation

// configure sets the per-OS application options: closing the last window quits the application (as on Windows;
// there is no menu bar item or Dock state to come back to: spec/architecture/desktop.md §2).
func configure(o *application.Options) {
	o.Mac.ApplicationShouldTerminateAfterLastWindowClosed = true
}

// updateOptions turns the automatic update on when the running bundle can be replaced where it is. Running from
// a disk image or a translocated copy (an app opened straight from Downloads) cannot: the update stays off, with
// the reason on stderr, until the user moves the app to /Applications (README.md).
func updateOptions(u *desktop.Updater) *app.UpdateOptions {
	if err := desktop.CheckCanInstall(); err != nil {
		fmt.Fprintln(os.Stderr, "mam: automatic update is off:", err)
		return nil
	}
	return &app.UpdateOptions{Install: u.Install}
}
