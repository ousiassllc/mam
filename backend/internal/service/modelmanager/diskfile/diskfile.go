// Package diskfile is what the model manager asks of a model file on the disk: whether it is all there and how much of it a partial download has. It is a
// package of its own so that modelmanager stays within its size (linterly).
package diskfile

import (
	"os"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// IsThere reports whether path is a regular file of exactly size bytes.
func IsThere(path string, size int64) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() == size
}

// PartSize is how many bytes of the download of dest are on the disk (at most total).
func PartSize(dest string, total int64) int64 {
	if st, err := os.Stat(fetch.PartPath(dest)); err == nil {
		return min(st.Size(), total)
	}
	return 0
}

// CheckSpace refuses a download of size bytes to dest when the disk below dir has not the room for what is still to come (nothing when the file is already there, and a
// partial download counts for the part it has). free is the free space at a path.
func CheckSpace(dest string, size int64, dir string, free func(string) uint64) error {
	if need := size - PartSize(dest, size); !IsThere(dest, size) && need > 0 {
		if have := free(dir); uint64(need) > have {
			return &fetch.NoSpaceError{Need: uint64(need), Free: have, Path: dir}
		}
	}
	return nil
}
