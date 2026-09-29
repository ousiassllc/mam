package fsutil

import (
	"fmt"
	"os"
)

// FreeBytes returns the space available to the current user on the volume that holds path. When
// path does not exist yet, the nearest existing parent is used.
func FreeBytes(path string) (uint64, error) {
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		parent := parentOf(path)
		if parent == path {
			return 0, fmt.Errorf("no existing parent of %s", path)
		}
		path = parent
	}
	return freeBytes(path)
}
