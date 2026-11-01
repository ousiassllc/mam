package engine

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
)

// ErrUnsafeArchive means the archive is not the one file the catalog describes: another name, a path, a link, more entries.
var ErrUnsafeArchive = errors.New("engine: the archive is not the pinned app-server")

// ErrExeMismatch means the extracted executable has not the size or SHA-256 the catalog pinned.
var ErrExeMismatch = errors.New("engine: the extracted app-server does not match the catalog")

const chunk = 1 << 20

// exeMode is the mode of the extracted executable: the owner may run it (the bit that macOS needs; Windows ignores it).
const exeMode = 0o700

// extractExe writes the single file of the archive (a zip or a tar.gz, by a.Format) to dir/a.Exe and checks its size
// and SHA-256 against the catalog. The archive must hold exactly that one regular file, named a.Exe at its top: a name
// with a separator, "..", a drive or an absolute path is refused, as is a link or a directory. On any failure the file
// is removed. progress receives the bytes written and the total.
func extractExe(ctx context.Context, archive, dir string, a catalog.Asset, progress func(done, total int64)) error {
	switch a.Format {
	case catalog.FormatZip:
		return extractZip(ctx, archive, dir, a, progress)
	case catalog.FormatTarGz:
		return extractTarGz(ctx, archive, dir, a, progress)
	}
	return fmt.Errorf("%w: format %q", ErrUnsafeArchive, a.Format)
}

// safeName reports whether an archive entry name is the bare executable name of the catalog.
func safeName(name string, a catalog.Asset) bool {
	return name == a.Exe && name != ".." && !strings.ContainsAny(name, `/\:`)
}

func extractZip(ctx context.Context, archive, dir string, a catalog.Asset, progress func(done, total int64)) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("engine: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = r.Close() }()
	if len(r.File) != 1 {
		return fmt.Errorf("%w: %d entries", ErrUnsafeArchive, len(r.File))
	}
	f := r.File[0]
	if !safeName(f.Name, a) || f.Mode()&os.ModeSymlink != 0 || !f.Mode().IsRegular() {
		return fmt.Errorf("%w: entry %q", ErrUnsafeArchive, f.Name)
	}
	if f.UncompressedSize64 != uint64(a.ExeSize) { //nolint:gosec // a catalog size, validated positive
		return fmt.Errorf("%w: size %d", ErrExeMismatch, f.UncompressedSize64)
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("engine: read %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	return writeExe(ctx, rc, dir, a, progress)
}

func extractTarGz(ctx context.Context, archive, dir string, a catalog.Asset, progress func(done, total int64)) (err error) {
	f, err := os.Open(archive) //nolint:gosec // the downloaded archive inside mam's own directory
	if err != nil {
		return fmt.Errorf("engine: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("engine: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	hdr, err := tr.Next()
	if err != nil { // io.EOF: an archive with no entry
		return fmt.Errorf("%w: first entry: %w", ErrUnsafeArchive, err)
	}
	if !safeName(hdr.Name, a) || hdr.Typeflag != tar.TypeReg {
		return fmt.Errorf("%w: entry %q (type %q)", ErrUnsafeArchive, hdr.Name, hdr.Typeflag)
	}
	if hdr.Size != a.ExeSize {
		return fmt.Errorf("%w: size %d", ErrExeMismatch, hdr.Size)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(filepath.Join(dir, a.Exe))
		}
	}()
	if err := writeExe(ctx, tr, dir, a, progress); err != nil {
		return err
	}
	// The one file must be the whole archive: nothing may follow it (the file is removed above when something does).
	switch _, nerr := tr.Next(); {
	case nerr == nil:
		return fmt.Errorf("%w: more than the one entry", ErrUnsafeArchive)
	case !errors.Is(nerr, io.EOF):
		return fmt.Errorf("engine: read the end of %s: %w", filepath.Base(archive), nerr)
	}
	return nil
}

// writeExe copies r, the one file of the archive, to dir/a.Exe and checks that it is as large as the catalog says and
// has its SHA-256. On any failure the file is removed.
func writeExe(ctx context.Context, r io.Reader, dir string, a catalog.Asset, progress func(done, total int64)) (err error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	target := filepath.Join(dir, a.Exe)
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, exeMode) //nolint:gosec // target is dir + a catalog-validated bare name
	if err != nil {
		return fmt.Errorf("engine: create %s: %w", a.Exe, err)
	}
	defer func() {
		err = errors.Join(err, out.Close())
		if err != nil {
			_ = os.Remove(target)
		}
	}()
	h := sha256.New()
	buf := make([]byte, chunk)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > a.ExeSize {
				return fmt.Errorf("%w: more than %d bytes", ErrExeMismatch, a.ExeSize)
			}
			h.Write(buf[:n])
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fmt.Errorf("engine: write %s: %w", a.Exe, werr)
			}
			if progress != nil {
				progress(done, a.ExeSize)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("engine: read %s: %w", a.Exe, rerr)
		}
	}
	if done != a.ExeSize || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.ExeSHA256) {
		return ErrExeMismatch
	}
	return nil
}
