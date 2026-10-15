// Package bundlezip is the ZIP file of a Project bundle (spec/requirements/non-functional.md §8):
// manifest.json, data.json with the Project Data Model, and the asset files. It writes a bundle
// (Export) and reads one (Open) and puts its files in the assets directory (Archive.Install); the
// database side of the bundle is repository/bundle, and the use case is service.BundleService.
package bundlezip

import (
	"fmt"
	"os"
)

// Limits of an archive. The bundle is a local file the user chose, but a broken or hostile archive
// must not fill the disk or memory.
const (
	MaxBytes     = 8 << 30 // the archive
	maxFileBytes = 2 << 30 // one file in it
	// maxExtractBytes is what an import writes to the assets directory at most, however much room
	// the disk has: the files the data names, added up.
	maxExtractBytes = 32 << 30
	maxEntries      = 100_000
)

// The permissions of the files and directories of the assets directory (the same as an upload).
const (
	filePerm os.FileMode = 0o600
	dirPerm  os.FileMode = 0o700
)

// InvalidError is an archive that cannot be imported. Message is meant for the user.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}
