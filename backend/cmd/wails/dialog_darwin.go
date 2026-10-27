//go:build darwin

package main

import "os/exec"

// showError shows a native error dialog and blocks until it is closed. It runs osascript, not a Wails dialog: those
// need the running Wails event loop, and startup errors happen before it, or while the process is about to exit.
func showError(text string) {
	// The title and the text are arguments of the script, never part of its source.
	_ = exec.Command("osascript", //nolint:gosec // fixed binary and script
		"-e", "on run argv",
		"-e", "display alert (item 1 of argv) message (item 2 of argv) as critical",
		"-e", "end run",
		appName, text).Run()
}
