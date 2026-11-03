package extract

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrTooLarge reports that an archive holds more bytes than the catalog pinned for it (a decompression bomb, or an archive that is
// not the pinned one).
var ErrTooLarge = errors.New("extract: the archive holds more than the catalog pinned")

// ErrExists reports that an entry would overwrite a file that is there: two parts of an environment must not write the same file.
var ErrExists = errors.New("extract: the archive writes a file that is there already")

// Zip extracts the zip archive below dest, which is created. strip is the single top directory inside the archive whose content is
// extracted ("" for none; an entry outside it is an error). limit is the number of uncompressed bytes the catalog pinned: an archive
// that holds more is refused before anything is written, and so is an entry that turns out larger than it declared. An existing file
// is never overwritten (ErrExists). Entries outside dest (".." and absolute paths) and links are refused. The caller removes a
// partial dest.
func Zip(ctx context.Context, archive, dest, strip string, limit int64, progress Progress) error {
	return unzip(ctx, archive, dest, limit, progress, func(name string) (string, bool, error) { return stripEntry(name, strip) })
}

// stripEntry is where the archive entry name goes below the destination: its clean slash path without the single top directory strip
// ("" for none; an entry outside it is an error; the directory itself gives ok false).
func stripEntry(name, strip string) (rel string, ok bool, err error) {
	clean, err := cleanEntry(name)
	if err != nil {
		return "", false, err
	}
	if strip == "" {
		return clean, true, nil
	}
	if clean == strip {
		return "", false, nil
	}
	rel, ok = strings.CutPrefix(clean, strip+"/")
	if !ok {
		return "", false, fmt.Errorf("extract: %q is not inside %q", name, strip)
	}
	return rel, true, nil
}

// Wheel extracts a Python wheel into siteDir, the way an installer does for a package that has no scripts: the files of the archive
// go to siteDir (with the .dist-info directory, which importlib.metadata reads), the purelib and platlib directories of
// "<name>.data" go to siteDir itself, and the rest of "<name>.data" (scripts, headers, data files: man pages) is not extracted. The
// limits and refusals are those of Zip.
func Wheel(ctx context.Context, archive, siteDir string, limit int64, progress Progress) error {
	return unzip(ctx, archive, siteDir, limit, progress, func(name string) (string, bool, error) {
		clean, err := cleanEntry(name)
		if err != nil {
			return "", false, err
		}
		top, rest, nested := strings.Cut(clean, "/")
		if !strings.HasSuffix(top, ".data") || !nested {
			return clean, true, nil
		}
		kind, rel, _ := strings.Cut(rest, "/")
		if (kind != "purelib" && kind != "platlib") || rel == "" {
			return "", false, nil
		}
		return rel, true, nil
	})
}

// cleanEntry is the slash path of an archive entry, refusing what would leave the destination.
func cleanEntry(name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	clean := path.Clean(name)
	if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	return clean, nil
}

// unzip writes the entries that place selects (ok false: the entry is skipped) below dest.
func unzip(ctx context.Context, archive, dest string, limit int64, progress Progress, place func(name string) (rel string, ok bool, err error)) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("extract: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = r.Close() }()
	var total uint64
	for _, f := range r.File {
		total += f.UncompressedSize64
	}
	if limit <= 0 || total > uint64(limit) {
		return fmt.Errorf("%w: %s holds %d bytes, the catalog pins %d", ErrTooLarge, filepath.Base(archive), total, limit)
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	var done int64
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, ok, err := place(f.Name)
		if err != nil {
			return err
		}
		if !ok || rel == "." {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		mode := f.Mode()
		switch {
		case f.FileInfo().IsDir():
			err = os.MkdirAll(target, 0o750)
		case mode&fs.ModeSymlink != 0 || !mode.IsRegular():
			return fmt.Errorf("extract: %s is not a regular file or directory: refused", f.Name)
		default:
			err = writeZipFile(ctx, f, target, func(n int64) {
				done += n
				if progress != nil {
					progress(done, int64(total)) //nolint:gosec // bounded by limit above
				}
			})
		}
		if err != nil {
			return err
		}
	}
	if progress != nil {
		progress(int64(total), int64(total)) //nolint:gosec // bounded by limit above
	}
	return nil
}

func writeZipFile(ctx context.Context, f *zip.File, target string, add func(int64)) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("extract: read %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	return writeEntry(ctx, rc, f.Name, target, int64(f.UncompressedSize64), f.Mode(), add) //nolint:gosec // bounded by the limit of the whole archive
}

// writeEntry writes the content of one archive entry to target, which must not exist (ErrExists). An entry may not hold more than its
// header says (declared): the header is what the limit was counted from. The file is readable by the runtime, and executable by its
// owner when the archive says so (the interpreter and the helper programs of a macOS or Linux package need the bit; a Windows archive
// has none).
func writeEntry(ctx context.Context, r io.Reader, name, target string, declared int64, mode fs.FileMode, add func(int64)) (err error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	perm := fs.FileMode(0o640)
	if mode&0o100 != 0 {
		perm = 0o750
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // target is below dest (cleanEntry); an extracted tree must stay readable by the runtime
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s", ErrExists, name)
	}
	if err != nil {
		return fmt.Errorf("extract: create %s: %w", name, err)
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	buf := make([]byte, chunk)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			written += int64(n)
			if written > declared {
				return fmt.Errorf("%w: %s is larger than its header says", ErrTooLarge, name)
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fmt.Errorf("extract: write %s: %w", name, werr)
			}
			add(int64(n))
		}
		if errors.Is(rerr, io.EOF) {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("extract: read %s: %w", name, rerr)
		}
	}
}
