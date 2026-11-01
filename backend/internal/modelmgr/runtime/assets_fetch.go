package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// ensure makes one asset the pinned one.
func (i *Installer) ensure(ctx context.Context, a catalog.Resource, state AssetState, report func(Phase, int64)) error {
	if state == AssetUnverified {
		ok, err := i.verifyFiles(ctx, a, func(d int64) { report(PhaseVerify, d) })
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	if err := i.download(ctx, a, func(d int64) { report(PhaseWeights, d) }); err != nil {
		return err
	}
	return nil // the download (and the extraction of an archive) checked the SHA-256 of what it wrote
}

// verifyFiles hashes the files of a that are there. It records the verified ones and deletes the ones that are not what the catalog
// pins (they are replaced by the download). It reports whether every file is verified.
func (i *Installer) verifyFiles(ctx context.Context, a catalog.Resource, progress func(done int64)) (bool, error) {
	all := true
	var done int64
	for _, f := range a.Files() {
		target := i.envPath(f.Dest)
		sum, err := hashFile(ctx, target, func(n int64) {
			if progress != nil {
				progress(done + n)
			}
		})
		done += f.Size
		switch {
		case errors.Is(err, os.ErrNotExist):
			all = false
			continue
		case err != nil:
			return false, err
		case sum != f.SHA256:
			all = false
			if err := os.Remove(target); err != nil {
				return false, err
			}
			continue
		}
		if err := i.stamp(f.Dest, target); err != nil {
			return false, err
		}
	}
	return all, nil
}

// stamp records that the file at target (the file dest of the catalog) was verified.
func (i *Installer) stamp(dest, target string) error {
	st, err := os.Stat(target)
	if err != nil {
		return err
	}
	return i.Store.Update(func(s *store.RuntimeState) {
		if s.Files == nil {
			s.Files = map[string]store.FileStamp{}
		}
		s.Files[dest] = stampOf(st)
	})
}

func hashFile(ctx context.Context, target string, progress func(n int64)) (string, error) {
	f, err := os.Open(target) //nolint:gosec // a path below the runtime's own directory
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		k, rerr := f.Read(buf)
		if k > 0 {
			h.Write(buf[:k])
			n += int64(k)
			if progress != nil {
				progress(n)
			}
		}
		if errors.Is(rerr, io.EOF) {
			return hex.EncodeToString(h.Sum(nil)), nil
		}
		if rerr != nil {
			return "", rerr
		}
	}
}

// download fetches a (a file to its place, an archive to the downloads directory and then its files out of it).
func (i *Installer) download(ctx context.Context, a catalog.Resource, progress func(done int64)) error {
	opts := i.Fetch
	opts.Progress = func(done, _ int64) { progress(done) }
	dest := i.assetDownloadPath(a)
	if err := fetch.Download(ctx, dest, fetch.Source{URL: a.URL, SHA256: a.SHA256, Size: a.Size}, opts); err != nil {
		return err
	}
	if !a.IsArchive() {
		return i.stamp(a.Dest, dest)
	}
	if err := i.unpack(ctx, a, dest); err != nil {
		return err
	}
	_ = os.Remove(dest)
	_ = os.Remove(i.Layout.DownloadsDir()) // only when empty
	return nil
}

// unpack puts the members of the downloaded archive in place, one directory at a time (exeinstall writes into one directory).
func (i *Installer) unpack(ctx context.Context, a catalog.Resource, archive string) error {
	byDir := map[string][]exeinstall.Member{}
	var dirs []string
	for _, m := range a.Members {
		target := i.envPath(m.Dest)
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) { // exeinstall never overwrites
			return err
		}
		dir := filepath.Dir(target)
		if _, ok := byDir[dir]; !ok {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], exeinstall.Member{Entry: m.Entry, Name: path.Base(m.Dest), Size: m.Size, SHA256: m.SHA256})
	}
	for _, dir := range dirs {
		if err := exeinstall.Extract(ctx, archive, a.Format, dir, byDir[dir], exeinstall.Options{}); err != nil {
			return fmt.Errorf("unpack %s: %w", path.Base(archive), err)
		}
	}
	for _, m := range a.Members {
		if err := i.stamp(m.Dest, i.envPath(m.Dest)); err != nil {
			return err
		}
	}
	return nil
}

// forgetAssets drops the record of verified files (the environment was replaced or removed).
func (i *Installer) forgetAssets() error {
	return i.Store.Update(func(s *store.RuntimeState) { s.Files = nil })
}
