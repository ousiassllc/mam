//go:build windows

package proc

import "log/slog"

// Windows ends the children with mam through the Job Object (group_windows.go): no guard process, no pid file.

func (r *run) guardAndNote(string, *slog.Logger) {}

func reclaimLeftover(Config) {}

// RunGuardIfRequested is false: there is no guard process on Windows.
func RunGuardIfRequested() bool { return false }
