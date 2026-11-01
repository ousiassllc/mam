// Package exeinstall is what the install of a downloaded program needs and has no say in which program it is: the safe
// extraction of the pinned files of an archive (a zip or a tar.gz) and the removal of the macOS quarantine attribute. The
// Codex app-server (internal/chatgpt/codex/engine) and ffmpeg (internal/media/ffmpegdl) are installed with it. The files
// are named, sized and hashed by the caller's catalog, which is compiled into mam; the archive decides none of that.
package exeinstall

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
)

// The archive formats.
const (
	FormatZip   = "zip"
	FormatTarGz = "tar.gz"
)

// ErrUnsafeArchive means the archive is not what the catalog describes: a file is missing or twice in it, one is not a
// regular file (a link, a directory), or, for an Exact extraction, there is more than the pinned files.
var ErrUnsafeArchive = errors.New("exeinstall: the archive is not what the catalog describes")

// ErrMismatch means an extracted file has not the size or SHA-256 the catalog pinned.
var ErrMismatch = errors.New("exeinstall: an extracted file does not match the catalog")

const chunk = 1 << 20

// fileMode is the mode of an extracted file: the owner may run it (the bit that macOS needs; Windows ignores it).
const fileMode = 0o700

// Member is one file of an archive that is wanted: where it is in the archive, what it is called when extracted, and what it
// must be.
type Member struct {
	// Entry is the exact name of the entry in the archive (slash separated, e.g. "ffmpeg-9.0.2-essentials_build/bin/ffmpeg.exe").
	Entry string
	// Name is the file name it is written under in the target directory: a bare name, never a path.
	Name string
	// Size and SHA256 (lower-case hex) are what the extracted file must be.
	Size   int64
	SHA256 string
}

// Options of Extract.
type Options struct {
	// Exact: the archive holds nothing but the members (no other file, no directory entry). Without it the other entries are
	// skipped, never read and never written.
	Exact bool
	// Progress receives the bytes written and the total of all members. It must not block.
	Progress func(done, total int64)
}

// Extract writes the members of the archive (of the given format) to dir, each as dir/Name, and checks every one against its
// pinned size and SHA-256. A member must be a regular file of the exact name Entry, once. The path of an entry is never used
// to write: a name with a separator, a drive or ".." in Name is refused before anything is written, and an entry that is
// not a member is skipped. On any failure every file written is removed again.
func Extract(ctx context.Context, archive, format, dir string, members []Member, o Options) (err error) {
	if len(members) == 0 {
		return fmt.Errorf("%w: nothing to extract", ErrUnsafeArchive)
	}
	for _, m := range members {
		if !bareName(m.Name) || m.Entry == "" || m.Size <= 0 {
			return fmt.Errorf("%w: member %q as %q", ErrUnsafeArchive, m.Entry, m.Name)
		}
	}
	w := &writer{ctx: ctx, dir: dir, members: members, progress: o.Progress}
	for _, m := range members {
		w.total += m.Size
	}
	defer func() {
		if err != nil {
			w.removeWritten()
		}
	}()
	switch format {
	case FormatZip:
		return w.zip(archive, o)
	case FormatTarGz:
		return w.tarGz(archive, o)
	}
	return fmt.Errorf("%w: format %q", ErrUnsafeArchive, format)
}

// bareName reports whether name is a file name with no path in it.
func bareName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\:`)
}

type writer struct {
	ctx      context.Context //nolint:containedctx // one extraction, the context lives as long as the call
	dir      string
	members  []Member
	total    int64
	done     int64
	progress func(done, total int64)
	written  []string
	seen     map[int]bool
}

// index is the member with this entry name; ok is false for an entry that is not wanted.
func (w *writer) index(entry string) (int, bool) {
	for i, m := range w.members {
		if m.Entry == entry {
			return i, true
		}
	}
	return 0, false
}

// claim marks member i as found; a second entry of the same name is not an archive the catalog describes.
func (w *writer) claim(i int) error {
	if w.seen == nil {
		w.seen = map[int]bool{}
	}
	if w.seen[i] {
		return fmt.Errorf("%w: %q twice", ErrUnsafeArchive, w.members[i].Entry)
	}
	w.seen[i] = true
	return nil
}

// missing is an error naming a member the archive does not hold.
func (w *writer) missing() error {
	for i, m := range w.members {
		if !w.seen[i] {
			return fmt.Errorf("%w: no entry %q", ErrUnsafeArchive, m.Entry)
		}
	}
	return nil
}

func (w *writer) removeWritten() {
	for _, p := range w.written {
		_ = os.Remove(p)
	}
}

func (w *writer) zip(archive string, o Options) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("exeinstall: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = r.Close() }()
	if o.Exact && len(r.File) != len(w.members) {
		return fmt.Errorf("%w: %d entries", ErrUnsafeArchive, len(r.File))
	}
	for _, f := range r.File {
		i, ok := w.index(f.Name)
		if !ok {
			if o.Exact {
				return fmt.Errorf("%w: entry %q", ErrUnsafeArchive, f.Name)
			}
			continue
		}
		if err := w.claim(i); err != nil {
			return err
		}
		m := w.members[i]
		if f.Mode()&os.ModeSymlink != 0 || !f.Mode().IsRegular() {
			return fmt.Errorf("%w: entry %q", ErrUnsafeArchive, f.Name)
		}
		if f.UncompressedSize64 != uint64(m.Size) { //nolint:gosec // a catalog size, validated positive
			return fmt.Errorf("%w: %s is %d bytes", ErrMismatch, m.Name, f.UncompressedSize64)
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("exeinstall: read %s: %w", f.Name, err)
		}
		err = w.write(rc, m)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return w.missing()
}

func (w *writer) tarGz(archive string, o Options) error {
	f, err := os.Open(archive) //nolint:gosec // the downloaded archive inside mam's own directory
	if err != nil {
		return fmt.Errorf("exeinstall: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("exeinstall: open %s: %w", filepath.Base(archive), err)
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return w.missing()
		}
		if err != nil {
			return fmt.Errorf("%w: read an entry: %w", ErrUnsafeArchive, err)
		}
		i, ok := w.index(hdr.Name)
		if !ok {
			if o.Exact {
				return fmt.Errorf("%w: entry %q (type %q)", ErrUnsafeArchive, hdr.Name, hdr.Typeflag)
			}
			continue
		}
		if err := w.claim(i); err != nil {
			return err
		}
		m := w.members[i]
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("%w: entry %q (type %q)", ErrUnsafeArchive, hdr.Name, hdr.Typeflag)
		}
		if hdr.Size != m.Size {
			return fmt.Errorf("%w: %s is %d bytes", ErrMismatch, m.Name, hdr.Size)
		}
		if err := w.write(tr, m); err != nil {
			return err
		}
	}
}

// write copies r, the one file of the archive, to dir/m.Name and checks that it is as large as the catalog says and has its
// SHA-256. The file is created new: an existing one is an error, never overwritten.
func (w *writer) write(r io.Reader, m Member) (err error) {
	if err := os.MkdirAll(w.dir, 0o750); err != nil {
		return fmt.Errorf("exeinstall: %w", err)
	}
	target := filepath.Join(w.dir, m.Name)
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode) //nolint:gosec // target is dir + a catalog-validated bare name
	if err != nil {
		return fmt.Errorf("exeinstall: create %s: %w", m.Name, err)
	}
	w.written = append(w.written, target)
	defer func() { err = errors.Join(err, out.Close()) }()
	h := sha256.New()
	buf := make([]byte, chunk)
	var size int64
	for {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			size += int64(n)
			if size > m.Size {
				return fmt.Errorf("%w: %s has more than %d bytes", ErrMismatch, m.Name, m.Size)
			}
			h.Write(buf[:n])
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fmt.Errorf("exeinstall: write %s: %w", m.Name, werr)
			}
			w.done += int64(n)
			if w.progress != nil {
				w.progress(w.done, w.total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("exeinstall: read %s: %w", m.Name, rerr)
		}
	}
	if size != m.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), m.SHA256) {
		return fmt.Errorf("%w: %s", ErrMismatch, m.Name)
	}
	return nil
}

// ErrNotWritable means a directory cannot be written to (a portable mam.exe in a read-only place, for example).
var ErrNotWritable = errors.New("exeinstall: the directory is not writable")

// CheckWritable creates root if needed and makes sure a file can be created in it.
func CheckWritable(root string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return fmt.Errorf("%w: %w", ErrNotWritable, err)
	}
	f, err := os.CreateTemp(root, ".write-test-*")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotWritable, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
