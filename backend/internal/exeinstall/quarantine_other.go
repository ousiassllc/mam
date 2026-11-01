//go:build !darwin

package exeinstall

import "errors"

// errNoAttr is never returned here: there is no quarantine attribute off macOS.
var errNoAttr = errors.New("exeinstall: no such attribute")

func removeXattr(string, string) error { return nil }
