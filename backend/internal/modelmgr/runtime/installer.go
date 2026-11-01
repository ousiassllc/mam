package runtime

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
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// Installer installs the pinned package of one runtime.
type Installer struct {
	Layout  store.RuntimeLayout
	Runtime catalog.Runtime
	// AssetSet is the weights and dependencies the runtime needs on disk (the zero value: none).
	AssetSet catalog.ResourceSet
	Store    *store.RuntimeStore
	Extract  extract.Extractor
	// Overlays are the scripts compiled into mam that an assembled runtime's build writes into the environment (catalog.Catalog.Overlays).
	Overlays map[string][]byte
	// Fetch are the options of the download (client, URL check; Progress is set per call).
	Fetch fetch.Options
	// FreeBytes reports the free space at a path (nil: fsutil.FreeBytes).
	FreeBytes func(path string) (uint64, error)
	Now       func() time.Time
	// Getenv reads the environment of mam for the launch (nil: os.Getenv).
	Getenv func(string) string
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
	// PinnedVersion is the version of the catalog. UpdateAvailable: it differs from Version, so installing again updates it.
	PinnedVersion   string `json:"pinned_version"`
	UpdateAvailable bool   `json:"update_available"`
	// Broken: the record says installed but the files are not there (deleted by hand); install again.
	Broken bool `json:"broken,omitempty"`
}

// ErrVariant means the build is not one of the runtime's.
var ErrVariant = errors.New("runtime: unknown build")

func (i *Installer) now() time.Time {
	if i.Now != nil {
		return i.Now().UTC()
	}
	return time.Now().UTC()
}

// PythonPath is the interpreter of the installed runtime.
func (i *Installer) PythonPath() string {
	return filepath.Join(i.Layout.EnvDir(), filepath.FromSlash(i.Runtime.Launch.Python))
}

// Status reads the record and checks the files.
func (i *Installer) Status() Status {
	st := Status{PinnedVersion: i.Runtime.Version}
	rec := i.Store.Get().Installed
	if rec == nil {
		return st
	}
	st.Installed, st.Version, st.Variant = true, rec.Version, rec.Variant
	if _, err := os.Stat(i.PythonPath()); err != nil {
		st.Installed, st.Broken = false, true
		return st
	}
	st.UpdateAvailable = rec.Version != i.Runtime.Version
	return st
}

// packageFile is a file of the package that is downloaded: the vendor's archive, or a part of an assembled build.
type packageFile struct {
	Path   string
	URL    string
	SHA256 string
	Size   int64
}

// packageFiles are the files the install of a build downloads, in the order they are extracted.
func (i *Installer) packageFiles(a catalog.RuntimeVariant) []packageFile {
	if !a.Assembled() {
		return []packageFile{{Path: filepath.Join(i.Layout.DownloadsDir(), path.Base(a.URL)), URL: a.URL, SHA256: a.SHA256, Size: a.Size}}
	}
	files := make([]packageFile, 0, len(a.Parts))
	for _, p := range a.Parts {
		files = append(files, packageFile{Path: filepath.Join(i.Layout.DownloadsDir(), p.Name+".zip"), URL: p.URL, SHA256: p.SHA256, Size: p.Size})
	}
	return files
}

// NeedBytes is the free space an install of the build needs: the part of the package that is not downloaded yet, and the extracted
// files (which exist next to the downloads until they are deleted).
func (i *Installer) NeedBytes(v catalog.Variant) (int64, error) {
	a, ok := i.Runtime.Variants[v]
	if !ok {
		return 0, ErrVariant
	}
	need := a.ExtractedSize
	for _, f := range i.packageFiles(a) {
		have := int64(0)
		if st, err := os.Stat(f.Path); err == nil && st.Size() == f.Size {
			have = f.Size // a part that was downloaded and kept by an interrupted install (it is verified again before it is used)
		} else if st, err := os.Stat(fetch.PartPath(f.Path)); err == nil {
			have = min(st.Size(), f.Size)
		}
		need += f.Size - have
	}
	return need, nil
}

// CheckSpace returns a *fetch.NoSpaceError when the disk of the runtime directory cannot hold the install.
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

// Install downloads, verifies and extracts the build, then swaps it in for the installed environment. A repeated call after an
// interruption resumes the download (a file that was downloaded whole is kept). On a failure the partial extraction is removed (the
// downloads are kept for a resume, unless the checksum was wrong). A build of parts is put together by mam itself (assemble).
func (i *Installer) Install(ctx context.Context, v catalog.Variant, progress Progress) error {
	a, ok := i.Runtime.Variants[v]
	if !ok {
		return ErrVariant
	}
	if err := i.Layout.CheckWritable(); err != nil {
		return err
	}
	if err := i.CheckSpace(v); err != nil {
		return err
	}
	files := i.packageFiles(a)
	opts := i.Fetch
	if opts.CheckURL == nil && a.Assembled() {
		opts.CheckURL = fetch.PackageURL // the parts of an assembled runtime come from more hosts than the weights (spec §13.2)
	}
	if err := download(ctx, files, a.Size, opts, progress); err != nil {
		return err
	}
	tmp := i.Layout.EnvNewDir()
	_ = os.RemoveAll(tmp)
	var err error
	if a.Assembled() {
		err = i.assemble(ctx, a, files, tmp, progress)
	} else {
		err = i.Extract.Extract(ctx, files[0].Path, tmp, i.Runtime.ArchiveRoot, func(done, total int64) {
			if progress != nil {
				progress(PhaseExtract, done, total)
			}
		})
	}
	if err == nil {
		err = i.swap(tmp)
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := i.Store.Update(func(s *store.RuntimeState) {
		s.Installed = &store.RuntimeRecord{Version: i.Runtime.Version, Variant: string(v), InstalledAt: i.now()}
	}); err != nil {
		return err
	}
	for _, f := range files {
		_ = os.Remove(f.Path)
	}
	_ = os.Remove(i.Layout.DownloadsDir()) // only when empty
	return i.forgetAssets()                // the files of the new environment are not the ones that were verified
}

// download fetches the files one after the other (each one resumed from its .part file and checked against its SHA-256 and size),
// reporting the bytes of all of them against total. A file that is there and verified is not fetched again.
func download(ctx context.Context, files []packageFile, total int64, opts fetch.Options, progress Progress) error {
	var base int64
	for _, f := range files {
		opts.Progress = func(done, _ int64) {
			if progress != nil {
				progress(PhaseDownload, base+done, total)
			}
		}
		if err := fetch.Download(ctx, f.Path, fetch.Source{URL: f.URL, SHA256: f.SHA256, Size: f.Size}, opts); err != nil {
			return err
		}
		base += f.Size
	}
	return nil
}

// swap makes tmp the environment; the previous one is removed once the new one is in place.
func (i *Installer) swap(tmp string) error {
	for _, rel := range []string{i.Runtime.Launch.Python, i.Runtime.Launch.Entry} {
		if _, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("runtime: the archive has no %s", rel)
		}
	}
	env, old := i.Layout.EnvDir(), i.Layout.EnvOldDir()
	_ = os.RemoveAll(old)
	if _, err := os.Stat(env); err == nil {
		if err := os.Rename(env, old); err != nil {
			return fmt.Errorf("runtime: replace the installed environment: %w", err)
		}
	}
	if err := os.Rename(tmp, env); err != nil {
		_ = os.Rename(old, env) // put the previous environment back
		return fmt.Errorf("runtime: put the new environment in place: %w", err)
	}
	_ = os.RemoveAll(old)
	return nil
}

// Remove deletes the environment, what was downloaded and the caches, and the record of the install (the setting of the auto start and
// the log stay). A runtime that is not installed is removed without an error.
func (i *Installer) Remove() error {
	for _, dir := range []string{i.Layout.EnvDir(), i.Layout.EnvNewDir(), i.Layout.EnvOldDir(), i.Layout.DownloadsDir(), i.Layout.CacheDir(), i.Layout.TempDir()} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("runtime: remove: %w", err)
		}
	}
	return i.Store.Update(func(s *store.RuntimeState) { s.Installed, s.Files = nil, nil })
}

// CleanLeftovers removes what an interrupted install or update left: a half-extracted env.new, and env.old (or puts it back when the
// swap stopped between its two renames).
func (i *Installer) CleanLeftovers() {
	_ = os.RemoveAll(i.Layout.EnvNewDir())
	env, old := i.Layout.EnvDir(), i.Layout.EnvOldDir()
	if _, err := os.Stat(old); err != nil {
		return
	}
	if _, err := os.Stat(env); errors.Is(err, os.ErrNotExist) {
		_ = os.Rename(old, env)
		return
	}
	_ = os.RemoveAll(old)
}
