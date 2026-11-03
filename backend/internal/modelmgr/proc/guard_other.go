//go:build !darwin && !windows

package proc

import "errors"

// startGuard: only macOS needs a guard (Linux has Pdeathsig).
func startGuard(int) error { return nil }

func runGuard(int, int) error { return errors.New("the guard runs on macOS only") }
