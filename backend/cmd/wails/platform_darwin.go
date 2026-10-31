//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// navigationCompleted is the window event of a finished navigation (the first one is the waiting page).
var navigationCompleted = events.Mac.WebViewDidFinishNavigation

// configure sets the per-OS application options: closing the last window quits the application (as on Windows;
// there is no menu bar item or Dock state to come back to: spec/architecture/desktop.md §2).
func configure(o *application.Options) {
	o.Mac.ApplicationShouldTerminateAfterLastWindowClosed = true
}
