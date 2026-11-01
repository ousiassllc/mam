package engine

import "errors"

// quarantineAttr is the extended attribute with which macOS marks a file that came from the network through a
// quarantine-aware program (a browser, for one). Gatekeeper checks a marked executable on its first launch and stops
// an unsigned or un-notarised one. mam's own HTTP client never sets it, so a fetched engine is not marked; clearing it
// is the second line of defence (a marked file would otherwise be stopped with no way to answer the dialog: the engine
// is a child process with no window).
const quarantineAttr = "com.apple.quarantine"

// clearQuarantine removes quarantineAttr from path. A file that has no such attribute is the normal case and no error.
// It does nothing off macOS (quarantine_other.go).
func clearQuarantine(path string) error {
	err := removeXattr(path, quarantineAttr)
	if err != nil && !errors.Is(err, errNoAttr) {
		return err
	}
	return nil
}
