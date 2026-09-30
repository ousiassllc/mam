package modeladapter

import (
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// Blob is the content of a generated file. A downloaded video is staged in a temporary file, not
// held in memory (a video is up to 300 MB and Takes are generated at the same time); BlobOf wraps
// bytes that are in memory anyway. The holder of a Blob MUST call StoreAs or Discard exactly once.
type Blob struct {
	data []byte
	path string // staged file; empty for an in-memory Blob
	size int64
}

// BlobOf returns an in-memory Blob.
func BlobOf(data []byte) Blob { return Blob{data: data, size: int64(len(data))} }

// Size is the length of the content in bytes.
func (b Blob) Size() int64 { return b.size }

// Bytes reads the whole content. It is for small files and tests; it does not consume the Blob.
func (b Blob) Bytes() ([]byte, error) {
	if b.path == "" {
		return b.data, nil
	}
	return os.ReadFile(b.path) // #nosec G304 -- a file this package staged itself
}

// Discard drops a Blob that is not stored: it removes the staged file.
func (b Blob) Discard() {
	if b.path != "" {
		_ = os.Remove(b.path)
	}
}

// StoreAs makes the content the file at dst, which only appears there complete (a staged file is
// renamed; it is copied through fsutil.WriteAtomic when dst is on another volume). The Blob is
// consumed whether or not StoreAs succeeds.
func (b Blob) StoreAs(dst string, perm fs.FileMode) error {
	if b.path == "" {
		return fsutil.WriteFileAtomic(dst, b.data, perm)
	}
	defer b.Discard() // no-op after a successful rename
	if err := os.Chmod(b.path, perm); err == nil {
		if err := os.Rename(b.path, dst); err == nil {
			return nil
		}
	}
	src, err := os.Open(b.path) // #nosec G304 -- a file this package staged itself
	if err != nil {
		return fmt.Errorf("open staged file: %w", err)
	}
	defer func() { _ = src.Close() }()
	return fsutil.WriteAtomic(dst, perm, func(w io.Writer) error {
		_, err := io.Copy(w, src)
		return err
	})
}
