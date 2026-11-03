package nodes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
)

// archivePath is where the archive of a node is downloaded to.
func (m *Manager) archivePath(n nodecat.Node) string {
	return filepath.Join(m.Layout.DownloadsDir(), n.Folder+"-"+short(n.Commit)+".zip")
}

// Install downloads the pinned archive of the node, checks its SHA-256 and size, extracts it below custom_nodes (an archive that holds more than
// the allowlist pinned, a path that leaves the folder or a link is refused), records the digest of the tree and puts the folder in place of
// the one that was there. A repeated call after an interruption resumes the download. On a failure the partial extraction is removed; the
// archive is kept for a resume unless its checksum was wrong. prepare (nil: none) runs once the extracted tree is checked and before the
// folder is replaced: the caller stops the engine there (a loaded node holds its files open); an error from it stops the install.
func (m *Manager) Install(ctx context.Context, id string, progress Progress, prepare func() error) error {
	n, ok := m.catalog().Node(id)
	switch {
	case !ok:
		return ErrUnknown
	case !n.Installable():
		return fmt.Errorf("%w: %s", ErrBlocked, n.InstallBlocked)
	}
	m.work.Lock()
	defer m.work.Unlock()
	if err := m.Layout.CheckWritable(); err != nil {
		return err
	}
	if err := m.CheckSpace(n); err != nil {
		return err
	}
	archive := m.archivePath(n)
	opts := m.Fetch
	if opts.FreeBytes == nil {
		opts.FreeBytes = m.FreeBytes
	}
	opts.Progress = func(done, total int64) {
		if progress != nil {
			progress(PhaseDownload, done, total)
		}
	}
	if err := fetch.Download(ctx, archive, fetch.Source{URL: n.ArchiveURL, SHA256: n.ArchiveSHA256, Size: n.ArchiveSize}, opts); err != nil {
		return err
	}
	// The bytes that are checked are the bytes that are extracted: the file is read once, here, so a replacement of it after the download
	// (by whoever can write the data directory) does not reach the extraction.
	held, err := readPinned(archive, n)
	if err != nil {
		return err
	}
	tmp := m.Folder(n) + ".new"
	_ = os.RemoveAll(tmp)
	unzip := m.Extract
	if unzip == nil {
		unzip = extract.ZipFrom
	}
	err = unzip(ctx, bytes.NewReader(held), int64(len(held)), tmp, n.ArchiveRoot(), n.ExtractedSize, func(done, total int64) {
		if progress != nil {
			progress(PhaseExtract, done, total)
		}
	})
	var sum string
	if err == nil {
		sum, err = m.seal(tmp)
	}
	if err == nil && prepare != nil {
		err = prepare()
	}
	if err == nil {
		err = m.swap(n, tmp)
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := m.rec.update(func(r *Record) {
		if r.Nodes == nil {
			r.Nodes = map[string]Installed{}
		}
		r.Nodes[n.ID] = Installed{Commit: n.Commit, ArchiveSHA256: n.ArchiveSHA256, TreeSHA256: sum, InstalledAt: m.now().UTC()}
	}); err != nil {
		return err
	}
	_ = os.Remove(archive)
	_ = os.Remove(m.Layout.DownloadsDir()) // only when empty
	return nil
}

// ErrArchiveChanged reports that the archive was not the pinned one when the install read it, after the download had checked it. The file is
// removed: the next install downloads it again.
var ErrArchiveChanged = errors.New("nodes: the downloaded archive changed after it was checked")

// readPinned reads the archive into memory (a node archive is at most tens of MB) and checks its size and SHA-256 against the pin again.
func readPinned(archive string, n nodecat.Node) ([]byte, error) {
	f, err := os.Open(archive) // #nosec G304 -- the archive this manager downloaded
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	held, err := io.ReadAll(io.LimitReader(f, n.ArchiveSize+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(held)
	if int64(len(held)) != n.ArchiveSize || !strings.EqualFold(hex.EncodeToString(sum[:]), n.ArchiveSHA256) {
		_ = os.Remove(archive)
		return nil, ErrArchiveChanged
	}
	return held, nil
}

// seal checks the extracted tree is a package ComfyUI can load and returns its digest.
func (m *Manager) seal(dir string) (string, error) {
	if st, err := os.Stat(filepath.Join(dir, "__init__.py")); err != nil || !st.Mode().IsRegular() {
		return "", ErrNotAPackage
	}
	return TreeDigest(dir)
}

// swap makes tmp the folder of the node; the previous folder is removed once the new one is in place, and put back when that fails.
func (m *Manager) swap(n nodecat.Node, tmp string) error {
	dest, old := m.Folder(n), m.Folder(n)+".old"
	_ = os.RemoveAll(old)
	if _, err := os.Lstat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return fmt.Errorf("nodes: replace the installed %s: %w", n.Folder, err)
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Rename(old, dest)
		return fmt.Errorf("nodes: put %s in place: %w", n.Folder, err)
	}
	_ = os.RemoveAll(old)
	return nil
}

// Remove deletes the folder of the node and its record (the consent stays). A node that was never installed is ErrNotInstalled. The engine
// has to be stopped by the caller: on Windows a folder with a loaded module cannot be deleted.
func (m *Manager) Remove(id string) error {
	n, ok := m.catalog().Node(id)
	if !ok {
		return ErrUnknown
	}
	m.work.Lock()
	defer m.work.Unlock()
	_, recorded := m.record().Nodes[id]
	_, statErr := os.Lstat(m.Folder(n))
	if !recorded && statErr != nil {
		return ErrNotInstalled
	}
	var failed error
	for _, p := range []string{m.Folder(n), m.Folder(n) + ".new", m.Folder(n) + ".old"} {
		if err := os.RemoveAll(p); err != nil {
			failed = errors.Join(failed, err)
		}
	}
	if failed != nil {
		return failed
	}
	return m.rec.update(func(r *Record) { delete(r.Nodes, id) })
}

// CleanLeftovers removes what an interrupted install left: a half-extracted <folder>.new, and <folder>.old (which is put back when the swap
// stopped between its two renames).
func (m *Manager) CleanLeftovers() {
	for _, n := range m.catalog().Nodes {
		dest := m.Folder(n)
		_ = os.RemoveAll(dest + ".new")
		if _, err := os.Lstat(dest + ".old"); err == nil {
			if _, err := os.Lstat(dest); err != nil {
				_ = os.Rename(dest+".old", dest)
			} else {
				_ = os.RemoveAll(dest + ".old")
			}
		}
	}
}
