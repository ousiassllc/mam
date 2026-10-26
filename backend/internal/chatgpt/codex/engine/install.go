package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// Phase is what Install is doing.
type Phase string

// The phases of Install, in order.
const (
	PhaseDownload Phase = "download"
	PhaseExtract  Phase = "extract"
)

// Progress receives the phase and the bytes done and in all of that phase. It must not block.
type Progress func(p Phase, done, total int64)

// Installer installs the pinned app-server of one architecture.
type Installer struct {
	Layout  Layout
	Version string
	Asset   catalog.Asset
	// Fetch are the options of the download (client, free space; Progress is set per call).
	Fetch fetch.Options
	// FreeBytes reports the free space at a path (nil: fsutil.FreeBytes).
	FreeBytes func(path string) (uint64, error)
	Now       func() time.Time
}

// Record is what an install left: the version, and the executable as it was verified.
type Record struct {
	Version     string    `json:"version"`
	Exe         string    `json:"exe"`
	ExeSize     int64     `json:"exe_size"`
	ExeSHA256   string    `json:"exe_sha256"`
	InstalledAt time.Time `json:"installed_at"`
}

// Status is what is installed against what is pinned.
type Status struct {
	Installed bool
	Version   string
	// PinnedVersion is the version of the catalog. UpdateAvailable: it differs from Version; mam runs only the
	// pinned version, so the installed one is not used until the engine is installed again.
	PinnedVersion   string
	UpdateAvailable bool
	// Broken: the record says installed but the file is not there or has another size (deleted or changed by hand).
	Broken bool
}

func (i *Installer) now() time.Time {
	if i.Now != nil {
		return i.Now().UTC()
	}
	return time.Now().UTC()
}

func (i *Installer) record() (Record, bool) {
	b, err := os.ReadFile(i.Layout.RecordPath())
	if err != nil {
		return Record{}, false
	}
	var r Record
	if json.Unmarshal(b, &r) != nil || r.Version == "" || r.Exe == "" {
		return Record{}, false
	}
	return r, true
}

// Status reads the record and checks the file (its presence and size: the SHA-256 was checked at the install).
func (i *Installer) Status() Status {
	st := Status{PinnedVersion: i.Version}
	rec, ok := i.record()
	if !ok {
		return st
	}
	st.Version = rec.Version
	info, err := os.Stat(filepath.Join(i.Layout.EngineDir(), rec.Exe))
	if err != nil || !info.Mode().IsRegular() || info.Size() != rec.ExeSize {
		st.Broken = true
		return st
	}
	st.Installed = true
	st.UpdateAvailable = rec.Version != i.Version
	return st
}

// ExePath is the executable of the installed engine of the pinned version; ok is false when it is not installed, is
// broken or is another version.
func (i *Installer) ExePath() (string, bool) {
	st := i.Status()
	if !st.Installed || st.UpdateAvailable {
		return "", false
	}
	rec, _ := i.record()
	return filepath.Join(i.Layout.EngineDir(), rec.Exe), true
}

func (i *Installer) archivePath() string {
	return filepath.Join(i.Layout.DownloadsDir(), path.Base(i.Asset.URL))
}

// NeedBytes is the free space an install needs: the part of the zip that is not downloaded yet, and the executable
// (which exists next to the zip until it is deleted).
func (i *Installer) NeedBytes() int64 {
	have := int64(0)
	if st, err := os.Stat(fetch.PartPath(i.archivePath())); err == nil {
		have = min(st.Size(), i.Asset.Size)
	}
	return i.Asset.Size - have + i.Asset.ExeSize
}

// CheckSpace returns a *fetch.NoSpaceError when the disk cannot hold the install.
func (i *Installer) CheckSpace() error {
	free := i.FreeBytes
	if free == nil {
		free = fsutil.FreeBytes
	}
	need := i.NeedBytes()
	have, err := free(i.Layout.Root)
	if err != nil {
		return fmt.Errorf("free space: %w", err)
	}
	if uint64(need) > have { //nolint:gosec // need is a size, never negative
		return &fetch.NoSpaceError{Need: uint64(need), Free: have, Path: i.Layout.Root} //nolint:gosec // same
	}
	return nil
}

// Install downloads the pinned zip, verifies its SHA-256 and size, extracts the executable, verifies that too, and swaps
// it in for the installed one. A repeated call after an interruption resumes the download. A file that does not match
// is never left behind: the part file of a wrong checksum is removed, and so is a half-extracted engine.
func (i *Installer) Install(ctx context.Context, progress Progress) error {
	if err := i.Layout.CheckWritable(); err != nil {
		return err
	}
	if err := i.CheckSpace(); err != nil {
		return err
	}
	archive := i.archivePath()
	opts := i.Fetch
	opts.Progress = func(done, total int64) {
		if progress != nil {
			progress(PhaseDownload, done, total)
		}
	}
	if err := fetch.Download(ctx, archive, fetch.Source{URL: i.Asset.URL, SHA256: i.Asset.SHA256, Size: i.Asset.Size}, opts); err != nil {
		return err
	}
	tmp := i.Layout.EngineNewDir()
	_ = os.RemoveAll(tmp)
	err := extractExe(ctx, archive, tmp, i.Asset, func(done, total int64) {
		if progress != nil {
			progress(PhaseExtract, done, total)
		}
	})
	if err == nil {
		err = i.swap(tmp)
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
		if errors.Is(err, ErrExeMismatch) || errors.Is(err, ErrUnsafeArchive) {
			_ = os.Remove(archive) // the zip matched its checksum but is not what the catalog describes: download it again
		}
		return err
	}
	if err := i.writeRecord(); err != nil {
		return err
	}
	_ = os.Remove(archive)
	_ = os.Remove(i.Layout.DownloadsDir()) // only when empty
	return nil
}

func (i *Installer) writeRecord() error {
	b, err := json.MarshalIndent(Record{
		Version: i.Version, Exe: i.Asset.Exe, ExeSize: i.Asset.ExeSize, ExeSHA256: i.Asset.ExeSHA256, InstalledAt: i.now(),
	}, "", " ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(i.Layout.RecordPath(), b, 0o600)
}

// swap makes tmp the engine; the previous engine is removed once the new one is in place.
func (i *Installer) swap(tmp string) error {
	engine, old := i.Layout.EngineDir(), i.Layout.EngineOldDir()
	_ = os.RemoveAll(old)
	if _, err := os.Stat(engine); err == nil {
		if err := os.Rename(engine, old); err != nil {
			return fmt.Errorf("engine: replace the installed engine: %w", err)
		}
	}
	if err := os.Rename(tmp, engine); err != nil {
		_ = os.Rename(old, engine) // put the previous engine back
		return fmt.Errorf("engine: put the new engine in place: %w", err)
	}
	_ = os.RemoveAll(old)
	return nil
}

// Remove deletes the engine, its record and what is left of a download.
func (i *Installer) Remove() error {
	if err := os.RemoveAll(i.Layout.EngineDir()); err != nil {
		return fmt.Errorf("engine: remove: %w", err)
	}
	_ = os.RemoveAll(i.Layout.EngineNewDir())
	_ = os.RemoveAll(i.Layout.EngineOldDir())
	_ = os.RemoveAll(i.Layout.DownloadsDir())
	if err := os.Remove(i.Layout.RecordPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("engine: remove the record: %w", err)
	}
	_ = os.Remove(i.Layout.Root) // only when empty
	return nil
}

// CleanLeftovers removes what an interrupted install left: a half-extracted engine.new, and engine.old (or puts it back
// when the swap stopped between its two renames).
func (i *Installer) CleanLeftovers() {
	_ = os.RemoveAll(i.Layout.EngineNewDir())
	engine, old := i.Layout.EngineDir(), i.Layout.EngineOldDir()
	if _, err := os.Stat(old); err != nil {
		return
	}
	if _, err := os.Stat(engine); errors.Is(err, os.ErrNotExist) {
		_ = os.Rename(old, engine)
		return
	}
	_ = os.RemoveAll(old)
}
