package ffmpegdl

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// File is one installed executable as it was verified.
type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Record is what an install left: the version, and the executables as they were verified.
type Record struct {
	Version     string    `json:"version"`
	Files       []File    `json:"files"`
	InstalledAt time.Time `json:"installed_at"`
}

// DiskStatus is what is installed against what is pinned.
type DiskStatus struct {
	Installed bool
	Version   string
	// PinnedVersion is the version of the catalog. UpdateAvailable: it differs from Version. The installed copy keeps being
	// used (an older ffmpeg is still an ffmpeg); installing again replaces it.
	PinnedVersion   string
	UpdateAvailable bool
	// Broken: the record says installed but a file is not there or has another size (deleted or changed by hand). A broken
	// copy is not used.
	Broken bool
}

func (i *Installer) now() time.Time {
	if i.Now != nil {
		return i.Now().UTC()
	}
	return time.Now().UTC()
}

func (i *Installer) record() (Record, bool) {
	if i.Layout.Root == "" {
		return Record{}, false
	}
	b, err := os.ReadFile(i.Layout.RecordPath())
	if err != nil {
		return Record{}, false
	}
	var r Record
	if json.Unmarshal(b, &r) != nil || r.Version == "" || len(r.Files) == 0 {
		return Record{}, false
	}
	return r, true
}

// Status reads the record and checks the files (their presence and size: the SHA-256 was checked at the install). A record
// that does not vouch for both executables is Broken: the search gets the directory as a whole, so an executable that the
// record leaves out (put there by hand) must not be found through it.
func (i *Installer) Status() DiskStatus {
	st := DiskStatus{PinnedVersion: i.Platform.Version}
	rec, ok := i.record()
	if !ok {
		return st
	}
	st.Version = rec.Version
	vouched := map[string]bool{}
	for _, f := range rec.Files {
		info, err := os.Stat(filepath.Join(i.Layout.BinDir(), f.Name))
		if f.Name != filepath.Base(f.Name) || err != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
			st.Broken = true
			return st
		}
		vouched[f.Name] = true
	}
	for _, m := range i.Platform.Members() {
		if !vouched[m.Name] {
			st.Broken = true
			return st
		}
	}
	st.Installed = true
	st.UpdateAvailable = rec.Version != i.Platform.Version
	return st
}

// Dirs is the directory to give the search of media/locate (Finder.Extra): BinDir while a complete install is there, nothing
// otherwise. Files in BinDir that no record vouches for (a half-done install, a file put there by hand) are never found.
func (i *Installer) Dirs() []string {
	if !i.Status().Installed {
		return nil
	}
	return []string{i.Layout.BinDir()}
}

func (i *Installer) archivePath(a Archive) string {
	return filepath.Join(i.Layout.DownloadsDir(), path.Base(a.URL))
}

// NeedBytes is the free space an install needs: the part of the archives that is not downloaded yet, and the executables
// (which exist next to the archives until these are deleted).
func (i *Installer) NeedBytes() int64 {
	need := i.Platform.ExtractedBytes()
	for _, a := range i.Platform.Archives {
		have := int64(0)
		if st, err := os.Stat(fetch.PartPath(i.archivePath(a))); err == nil {
			have = min(st.Size(), a.Size)
		} else if st, err := os.Stat(i.archivePath(a)); err == nil {
			have = min(st.Size(), a.Size)
		}
		need += a.Size - have
	}
	return need
}

// CheckSpace returns a *fetch.NoSpaceError when the disk cannot hold the install.
func (i *Installer) CheckSpace() error {
	return exeinstall.CheckSpace(i.Layout.Root, i.NeedBytes(), i.FreeBytes)
}

func (i *Installer) writeRecord() error {
	files := make([]File, 0, 2)
	for _, m := range i.Platform.Members() {
		files = append(files, File{Name: m.Name, Size: m.Size, SHA256: m.SHA256})
	}
	b, err := json.MarshalIndent(Record{Version: i.Platform.Version, Files: files, InstalledAt: i.now()}, "", " ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(i.Layout.RecordPath(), b, 0o600)
}

// CleanLeftovers removes what an interrupted install left: a half-extracted bin.new, and bin.old (or puts it back when the
// swap stopped between its two renames).
func (i *Installer) CleanLeftovers() {
	if i.Layout.Root == "" {
		return
	}
	exeinstall.CleanLeftovers(i.Layout.BinNewDir(), i.Layout.BinDir(), i.Layout.BinOldDir())
}

// Remove deletes the installed ffmpeg, its record and what is left of a download.
func (i *Installer) Remove() error {
	if i.Layout.Root == "" {
		return nil
	}
	// The record goes first: with it gone nothing in bin is used any more, whatever the removal of the rest does.
	if err := os.Remove(i.Layout.RecordPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("ffmpegdl: remove the record: %w", err)
	}
	for _, d := range []string{i.Layout.BinDir(), i.Layout.BinNewDir(), i.Layout.BinOldDir(), i.Layout.DownloadsDir()} {
		if err := os.RemoveAll(d); err != nil {
			return fmt.Errorf("ffmpegdl: remove %s: %w", filepath.Base(d), err)
		}
	}
	_ = os.Remove(i.Layout.Root) // only when empty
	return nil
}
