// Package extract unpacks the ComfyUI portable archive (spec/architecture/model-manager/model-manager.md §3.3). The upstream
// packages are 7z archives made with `-m0=lzma2 -mx=9 -mfb=128 -md=768m -ms=on -mf=BCJ2` (LZMA2 and the BCJ2
// filter), which the standard library cannot read: SevenZip uses github.com/bodgit/sevenzip (pure Go, no
// cgo, LZMA2 + BCJ2). The Extractor interface keeps the library behind one seam.
package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
)

// Progress receives the uncompressed bytes written so far and their total. It must not block.
type Progress func(done, total int64)

// Extractor unpacks an archive.
type Extractor interface {
	// Extract writes the content of the archive below dest, without its single top directory root (a file
	// whose path does not start with root is an error). dest must be empty or missing. Entries that would
	// leave dest (".." and absolute paths) and symbolic links are refused. ctx stops the work between files
	// and between chunks; the caller removes a partial dest.
	Extract(ctx context.Context, archive, dest, root string, progress Progress) error
}

// SevenZip is the Extractor of 7z archives.
type SevenZip struct{}

var _ Extractor = SevenZip{}

// ErrUnsafePath reports that an archive entry would be written outside the destination.
var ErrUnsafePath = errors.New("extract: the archive has an entry outside the destination")

const chunk = 1 << 20

// Extract implements Extractor.
func (SevenZip) Extract(ctx context.Context, archive, dest, root string, progress Progress) error {
	r, err := sevenzip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("extract: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = r.Close() }()
	var total int64
	for _, f := range r.File {
		total += int64(f.UncompressedSize) //nolint:gosec // a size inside an archive that was just opened
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	var done int64
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		target, skip, err := entryPath(dest, root, f.Name)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		mode := f.FileInfo().Mode()
		switch {
		case f.FileInfo().IsDir():
			err = os.MkdirAll(target, 0o750)
		case mode&fs.ModeSymlink != 0 || !mode.IsRegular():
			return fmt.Errorf("extract: %s is not a regular file or directory: refused", f.Name)
		default:
			err = writeFile(ctx, f, target, func(n int64) {
				done += n
				if progress != nil {
					progress(done, total)
				}
			})
		}
		if err != nil {
			return err
		}
	}
	if progress != nil {
		progress(total, total)
	}
	return nil
}

// entryPath is where the archive entry name goes below dest. skip is true for the root directory itself.
func entryPath(dest, root, name string) (target string, skip bool, err error) {
	name = strings.ReplaceAll(name, `\`, "/")
	clean := path.Clean(name)
	if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") {
		return "", false, fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	if clean == root {
		return "", true, nil
	}
	rel, ok := strings.CutPrefix(clean, root+"/")
	if !ok {
		return "", false, fmt.Errorf("extract: %q is not inside %q", name, root)
	}
	return filepath.Join(dest, filepath.FromSlash(rel)), false, nil
}

func writeFile(ctx context.Context, f *sevenzip.File, target string, add func(int64)) (err error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("extract: read %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640) //nolint:gosec // target is below dest (entryPath); an extracted tree must stay readable by the engine
	if err != nil {
		return fmt.Errorf("extract: create %s: %w", f.Name, err)
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	buf := make([]byte, chunk)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := rc.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fmt.Errorf("extract: write %s: %w", f.Name, werr)
			}
			add(int64(n))
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("extract: read %s: %w", f.Name, rerr)
		}
	}
}
