package extract

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// TarGz extracts the gzip-compressed tar archive below dest, which is created. strip is the single top directory inside the archive whose
// content is extracted ("" for none; an entry outside it is an error). limit is the number of uncompressed bytes the catalog pinned: the
// headers are summed as they are read, so an archive that holds more is refused before the entry that crosses the limit is written, and so
// is an entry that turns out larger than it declared. An existing file is never overwritten (ErrExists). Entries outside dest (".." and
// absolute paths), hard links, devices and the like are refused. A symbolic link is not created and not an error: the links of a
// relocatable Python (python -> python3.13, the man pages) are aliases, and what mam launches is named by the catalog. The owner-execute
// bit of a file is kept. The caller removes a partial dest.
func TarGz(ctx context.Context, archive, dest, strip string, limit int64, progress Progress) error {
	f, err := os.Open(archive) //nolint:gosec // a file the model manager downloaded into its own directory
	if err != nil {
		return fmt.Errorf("extract: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("extract: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = zr.Close() }()
	if limit <= 0 {
		return fmt.Errorf("%w: %s has no pinned size", ErrTooLarge, filepath.Base(archive))
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	tr := tar.NewReader(zr)
	var declared, done int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("extract: read %s: %w", filepath.Base(archive), err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader || h.Typeflag == tar.TypeSymlink {
			continue
		}
		rel, ok, err := stripEntry(h.Name, strip)
		if err != nil {
			return err
		}
		if !ok || rel == "." {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o750)
		case tar.TypeReg:
			if declared += h.Size; declared > limit {
				return fmt.Errorf("%w: %s holds more than %d bytes", ErrTooLarge, filepath.Base(archive), limit)
			}
			err = writeEntry(ctx, tr, h.Name, target, h.Size, h.FileInfo().Mode(), func(n int64) {
				done += n
				if progress != nil {
					progress(done, limit)
				}
			})
		default:
			return fmt.Errorf("extract: %s is not a regular file or directory: refused", h.Name)
		}
		if err != nil {
			return err
		}
	}
	if progress != nil {
		progress(done, done)
	}
	return nil
}
