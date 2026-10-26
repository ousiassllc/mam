package engine

import (
	"archive/zip"
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

// ErrUnsafeArchive means the zip is not the one file the catalog describes: another name, a path, more entries.
var ErrUnsafeArchive = errors.New("engine: the archive is not the pinned app-server")

// ErrExeMismatch means the extracted executable has not the size or SHA-256 the catalog pinned.
var ErrExeMismatch = errors.New("engine: the extracted app-server does not match the catalog")

const chunk = 1 << 20

// extractExe writes the single file of the zip to dir/a.Exe and checks its size and SHA-256 against the catalog. The
// zip must hold exactly that one regular file, named a.Exe at its top: a name with a separator, "..", a drive or an
// absolute path is refused, as is a link. On any failure the file is removed. progress receives the bytes written and the total.
func extractExe(ctx context.Context, archive, dir string, a catalog.Asset, progress func(done, total int64)) (err error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("engine: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = r.Close() }()
	if len(r.File) != 1 {
		return fmt.Errorf("%w: %d entries", ErrUnsafeArchive, len(r.File))
	}
	f := r.File[0]
	if f.Name != a.Exe || strings.ContainsAny(f.Name, `/\:`) || f.Name == ".." || f.Mode()&os.ModeSymlink != 0 || !f.Mode().IsRegular() {
		return fmt.Errorf("%w: entry %q", ErrUnsafeArchive, f.Name)
	}
	if f.UncompressedSize64 != uint64(a.ExeSize) { //nolint:gosec // a catalog size, validated positive
		return fmt.Errorf("%w: size %d", ErrExeMismatch, f.UncompressedSize64)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	target := filepath.Join(dir, a.Exe)
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700) //nolint:gosec // target is dir + a catalog-validated bare name
	if err != nil {
		return fmt.Errorf("engine: create %s: %w", a.Exe, err)
	}
	defer func() {
		err = errors.Join(err, out.Close())
		if err != nil {
			_ = os.Remove(target)
		}
	}()
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("engine: read %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	h := sha256.New()
	buf := make([]byte, chunk)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := rc.Read(buf)
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
			return fmt.Errorf("engine: read %s: %w", f.Name, rerr)
		}
	}
	if done != a.ExeSize || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.ExeSHA256) {
		return ErrExeMismatch
	}
	return nil
}
