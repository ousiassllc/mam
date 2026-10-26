// Package engine installs and describes the bundled ComfyUI (spec/architecture/model-manager/model-manager.md §3.3): it downloads
// the pinned portable archive of the GPU's build from the official release, checks its SHA-256, extracts it next
// to mam.exe, and says what is installed against what the catalog pins. mam ships none of ComfyUI's bytes.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// Installer installs the pinned engine.
type Installer struct {
	Layout  store.Layout
	Engine  catalog.Engine
	Store   *store.Store
	Extract extract.Extractor
	// Fetch are the options of the download (client, free space, progress is set per call).
	Fetch fetch.Options
	// FreeBytes reports the free space at a path (nil: fsutil.FreeBytes).
	FreeBytes func(path string) (uint64, error)
	Now       func() time.Time
}

// Phase is what Install is doing.
type Phase string

// The phases of Install, in order.
const (
	PhaseDownload Phase = "download"
	PhaseExtract  Phase = "extract"
)

// Progress receives the phase and the bytes done and in all of that phase. It must not block.
type Progress func(p Phase, done, total int64)

// Status is what is installed against what is pinned.
type Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Variant   string `json:"variant,omitempty"`
	CPU       bool   `json:"cpu,omitempty"`
	// PinnedVersion is the version of the catalog. UpdateAvailable: it differs from Version, so installing the
	// engine again updates it.
	PinnedVersion   string `json:"pinned_version"`
	UpdateAvailable bool   `json:"update_available"`
	// Broken: the record says installed but the files are not there (deleted by hand); install again.
	Broken bool `json:"broken,omitempty"`
}

// Errors of Install.
var (
	// ErrVariant: the build is not one of the catalog's.
	ErrVariant = errors.New("engine: unknown build")
	// ErrBusyRunning: the engine is running; stop it before replacing it.
	ErrBusyRunning = errors.New("engine: stop the running engine first")
)

func (i *Installer) now() time.Time {
	if i.Now != nil {
		return i.Now().UTC()
	}
	return time.Now().UTC()
}

// Status reads the record and checks the files.
func (i *Installer) Status() Status {
	st := Status{PinnedVersion: i.Engine.Version}
	rec := i.Store.Get().Engine
	if rec == nil {
		return st
	}
	st.Installed, st.Version, st.Variant, st.CPU = true, rec.Version, rec.Variant, rec.CPU
	if _, err := os.Stat(i.PythonPath()); err != nil {
		st.Installed, st.Broken = false, true
		return st
	}
	st.UpdateAvailable = rec.Version != i.Engine.Version
	return st
}

// PythonPath and MainPath are the interpreter and the script of the installed engine.
func (i *Installer) PythonPath() string {
	return filepath.Join(i.Layout.EngineDir(), filepath.FromSlash(i.Engine.Python))
}

// MainPath is the main.py of ComfyUI inside the extracted engine.
func (i *Installer) MainPath() string {
	return filepath.Join(i.Layout.EngineDir(), filepath.FromSlash(i.Engine.Main))
}

// archivePath is where the archive of a build is downloaded to.
func (i *Installer) archivePath(a catalog.Asset) string {
	return filepath.Join(i.Layout.DownloadsDir(), path.Base(a.URL))
}

// NeedBytes is the free space an install of the build needs: the part of the archive that is not downloaded yet,
// and the extracted files (which exist next to the archive until it is deleted).
func (i *Installer) NeedBytes(v catalog.Variant) (int64, error) {
	a, ok := i.Engine.Variants[v]
	if !ok {
		return 0, ErrVariant
	}
	have := int64(0)
	if st, err := os.Stat(fetch.PartPath(i.archivePath(a))); err == nil {
		have = min(st.Size(), a.Size)
	}
	return a.Size - have + a.ExtractedSize, nil
}

// CheckSpace returns a *fetch.NoSpaceError when the disk of the engine directory cannot hold the install.
func (i *Installer) CheckSpace(v catalog.Variant) error {
	need, err := i.NeedBytes(v)
	if err != nil {
		return err
	}
	free := i.FreeBytes
	if free == nil {
		free = fsutil.FreeBytes
	}
	have, err := free(i.Layout.Root)
	if err != nil {
		return fmt.Errorf("free space: %w", err)
	}
	if uint64(need) > have { //nolint:gosec // need is a size, never negative
		return &fetch.NoSpaceError{Need: uint64(need), Free: have, Path: i.Layout.Root} //nolint:gosec // same
	}
	return nil
}

// Install downloads, verifies and extracts the build, then swaps it in for the installed engine (the data
// directory is not touched). A repeated call after an interruption resumes the download. On a failure the
// partial extraction is removed (the archive and its part file are kept for a resume, unless the checksum
// was wrong).
func (i *Installer) Install(ctx context.Context, v catalog.Variant, cpu bool, progress Progress) error {
	a, ok := i.Engine.Variants[v]
	if !ok {
		return ErrVariant
	}
	if err := i.Layout.CheckWritable(); err != nil {
		return err
	}
	if err := i.CheckSpace(v); err != nil {
		return err
	}
	archive := i.archivePath(a)
	opts := i.Fetch
	opts.Progress = func(done, total int64) {
		if progress != nil {
			progress(PhaseDownload, done, total)
		}
	}
	if err := fetch.Download(ctx, archive, fetch.Source{URL: a.URL, SHA256: a.SHA256, Size: a.Size}, opts); err != nil {
		return err
	}
	tmp := i.Layout.EngineNewDir()
	_ = os.RemoveAll(tmp)
	err := i.Extract.Extract(ctx, archive, tmp, i.Engine.ArchiveRoot, func(done, total int64) {
		if progress != nil {
			progress(PhaseExtract, done, total)
		}
	})
	if err == nil {
		err = i.swap(tmp)
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := i.Store.Update(func(s *store.State) {
		s.Engine = &store.EngineRecord{Version: i.Engine.Version, Variant: string(v), CPU: cpu, InstalledAt: i.now()}
	}); err != nil {
		return err
	}
	_ = os.Remove(archive)
	_ = os.Remove(i.Layout.DownloadsDir()) // only when empty
	return nil
}

// swap makes tmp the engine; the previous engine is removed once the new one is in place.
func (i *Installer) swap(tmp string) error {
	if _, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(i.Engine.Python))); err != nil {
		return fmt.Errorf("engine: the archive has no %s", i.Engine.Python)
	}
	if _, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(i.Engine.Main))); err != nil {
		return fmt.Errorf("engine: the archive has no %s", i.Engine.Main)
	}
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

// Remove deletes the engine and its record; the data directory (models, outputs) and the record of the models stay.
func (i *Installer) Remove() error {
	if err := os.RemoveAll(i.Layout.EngineDir()); err != nil {
		return fmt.Errorf("engine: remove: %w", err)
	}
	_ = os.RemoveAll(i.Layout.DownloadsDir())
	return i.Store.Update(func(s *store.State) { s.Engine = nil })
}

// CleanLeftovers removes what an interrupted install or update left: a half-extracted engine.new, and engine.old
// (or puts it back when the swap stopped between its two renames).
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

// Command is the launch of the installed engine on port: the recipe of the portable build's run_*.bat
// (`<python dir>\python.exe -s ComfyUI\main.py`) with the flags of the bundled use: loopback only, no
// browser, no custom node (they run arbitrary code), every file below the data directory. --windows-standalone-build
// is left out: in this release it only opens the browser and enables the assets API, neither of which is wanted.
func (i *Installer) Command(port int) (proc.Spec, error) {
	rec := i.Store.Get().Engine
	if rec == nil || !i.Status().Installed {
		return proc.Spec{}, errors.New("ComfyUI エンジンが導入されていません")
	}
	args := []string{
		"-s", filepath.FromSlash(i.Engine.Main),
		"--listen", "127.0.0.1", "--port", fmt.Sprint(port),
		"--disable-auto-launch", "--disable-all-custom-nodes",
		"--base-directory", i.Layout.DataDir(),
	}
	if rec.CPU {
		args = append(args, "--cpu")
	}
	if err := os.MkdirAll(i.Layout.DataDir(), 0o750); err != nil {
		return proc.Spec{}, err
	}
	return proc.Spec{
		Command: i.PythonPath(), Args: args, Dir: i.Layout.EngineDir(), LogPath: i.Layout.LogPath(),
		Env: []string{"PYTHONUNBUFFERED=1", "PYTHONIOENCODING=utf-8", "PYTHONNOUSERSITE=1"},
	}, nil
}
