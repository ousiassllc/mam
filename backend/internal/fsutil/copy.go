package fsutil

import (
	"io"
	"os"
)

// CopyFile copies the file src to dst atomically (a temporary file in dst's directory, then a rename) with mode 0600.
func CopyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // callers pass paths they resolved themselves
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return WriteAtomic(dst, 0o600, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
}
