//go:build !darwin

package engine

import "errors"

// errNoAttr is never returned here: there is no quarantine attribute off macOS.
var errNoAttr = errors.New("engine: no such attribute")

func removeXattr(string, string) error { return nil }
