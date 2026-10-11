//go:build windows

package main

import "golang.org/x/sys/windows"

// showError shows a native error dialog and blocks until it is closed. It uses MessageBox
// directly, not the Wails dialogs: those need the running Wails event loop, and startup errors
// happen before it, or while the process is about to exit.
func showError(text string) {
	title, err := windows.UTF16PtrFromString(appName)
	if err != nil {
		return
	}
	body, err := windows.UTF16PtrFromString(text)
	if err != nil {
		body, _ = windows.UTF16PtrFromString("mam failed to start")
	}
	_, _ = windows.MessageBox(0, body, title, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}
