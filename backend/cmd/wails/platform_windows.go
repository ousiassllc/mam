//go:build windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/ousiassllc/mam/backend/internal/app"
	"github.com/ousiassllc/mam/backend/internal/desktop"
)

// navigationCompleted is the window event of a finished navigation (the first one is the waiting page).
var navigationCompleted = events.Windows.WebViewNavigationCompleted

// configure sets the per-OS application options: none on Windows (closing the last window quits by default).
func configure(*application.Options) {}

// updateOptions turns the automatic update on: the NSIS installer replaces the installed exe.
func updateOptions(u *desktop.Updater) *app.UpdateOptions {
	return &app.UpdateOptions{Install: u.Install}
}
