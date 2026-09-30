// Package fsutil holds filesystem helpers shared across layers.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	tempPrefix = "."
	tempSuffix = ".tmp"
)

// WriteFileAtomic writes data to path so that readers (and a crash) only ever see
// the old file or the complete new one (non-functional.md §3).
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	return WriteAtomic(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// WriteAtomic streams content produced by write into a temporary file in the
// destination directory, fsyncs it, then renames it over path. On any error the
// temporary file is removed and path is left untouched.
func WriteAtomic(path string, perm fs.FileMode, write func(w io.Writer) error) (err error) {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, tempPrefix+base+".*"+tempSuffix)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err = write(tmp); err != nil {
		return err
	}
	if err = tmp.Chmod(perm); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	syncDir(dir)
	return nil
}

// CreateStagingTemp creates a temporary file in dir (the system temporary directory when dir is
// empty) named so that RemoveStaleTemp removes it if the process dies before the file is moved
// into place or removed.
func CreateStagingTemp(dir string) (*os.File, error) {
	return os.CreateTemp(dir, tempPrefix+"staging."+"*"+tempSuffix)
}

// syncDir makes the rename durable. It is best effort: directories cannot be opened on Windows.
func syncDir(dir string) {
	d, err := os.Open(dir) //nolint:gosec // dir is the destination directory the caller already writes to
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// RemoveStaleTemp deletes temporary files left behind by WriteAtomic under root
// (for example after a crash between create and rename) and returns their paths.
func RemoveStaleTemp(root string) ([]string, error) {
	var removed []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		name := d.Name()
		if d.IsDir() || !strings.HasPrefix(name, tempPrefix) || !strings.HasSuffix(name, tempSuffix) {
			return nil
		}
		if err := os.Remove(path); err != nil { //nolint:gosec // root is the app-owned assets dir; only regular temp files are removed
			return err
		}
		removed = append(removed, path)
		return nil
	})
	return removed, err
}
