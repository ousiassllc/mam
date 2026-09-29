package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// browserCommand returns the OS-specific command that opens url in the default browser.
func browserCommand(goos, url string) (*exec.Cmd, error) {
	switch goos {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url), nil //nolint:gosec // fixed binary; url is the server's own loopback address
	case "darwin":
		return exec.Command("open", url), nil //nolint:gosec // fixed binary; url is the server's own loopback address
	case "linux", "freebsd", "openbsd", "netbsd":
		return exec.Command("xdg-open", url), nil //nolint:gosec // fixed binary; url is the server's own loopback address
	default:
		return nil, fmt.Errorf("opening a browser is not supported on %s", goos)
	}
}

// openBrowser starts the default browser without waiting for it to exit.
func openBrowser(url string) error {
	cmd, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start browser: %w", err)
	}
	go func() { _ = cmd.Wait() }() // reap the child; its exit status is irrelevant
	return nil
}
