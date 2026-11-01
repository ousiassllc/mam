package runtime

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// The phases of EnsureAssets.
const (
	// PhaseVerify hashes a file that is there against the catalog.
	PhaseVerify Phase = "verify"
	// PhaseWeights downloads a file that is missing or was not what the catalog pins.
	PhaseWeights Phase = "weights"
)

// AssetState is what the files of an asset are.
type AssetState string

// The states of an asset.
const (
	// AssetOK: every file is there and was verified (its SHA-256 was checked and it has not changed since).
	AssetOK AssetState = "ok"
	// AssetMissing: a file is not there or has another size than the catalog pins: download it.
	AssetMissing AssetState = "missing"
	// AssetUnverified: the files are there with the right size, but their SHA-256 was not checked (the package was just installed, or
	// a file was changed): EnsureAssets hashes them, and downloads what is not the pinned file.
	AssetUnverified AssetState = "unverified"
)

// AssetStatus is one asset of the catalog against the disk.
type AssetStatus struct {
	Name     string     `json:"name"`
	State    AssetState `json:"state"`
	Required bool       `json:"required"`
}

// AssetProgress receives the phase, the asset it is at and the bytes done of all the assets that still are to do (not of the
// asset). It must not block.
type AssetProgress func(ph Phase, name string, done, total int64)

// AssetError is the failure of one asset: Name says which, Err why (a *fetch.HTTPError, fetch.ErrChecksum, *fetch.NoSpaceError, ...).
type AssetError struct {
	Name string
	Err  error
}

func (e *AssetError) Error() string { return e.Name + ": " + e.Err.Error() }
func (e *AssetError) Unwrap() error { return e.Err }

func (i *Installer) envPath(rel string) string {
	return filepath.Join(i.Layout.EnvDir(), filepath.FromSlash(rel))
}

func stampOf(st os.FileInfo) store.FileStamp {
	return store.FileStamp{Size: st.Size(), ModTime: st.ModTime().UnixNano()}
}

// assetState compares the files of a with the disk and the record of what was verified.
func (i *Installer) assetState(a catalog.Resource, stamps map[string]store.FileStamp) AssetState {
	state := AssetOK
	for _, f := range a.Files() {
		st, err := os.Stat(i.envPath(f.Dest))
		switch {
		case err != nil || !st.Mode().IsRegular() || st.Size() != f.Size:
			return AssetMissing
		case stamps[f.Dest] != stampOf(st):
			state = AssetUnverified
		}
	}
	return state
}

// Assets lists the weights and dependencies of the runtime with what the disk says of each (a stat per file; nothing is hashed).
func (i *Installer) Assets() []AssetStatus {
	stamps := i.Store.Get().Files
	out := make([]AssetStatus, 0, len(i.AssetSet.Assets))
	for _, a := range i.AssetSet.Assets {
		out = append(out, AssetStatus{Name: a.Name, State: i.assetState(a, stamps), Required: a.Required})
	}
	return out
}

// NotReady lists the required assets that are not verified (missing, or not checked yet): the runtime must not start while there are
// any. Nothing is listed for a runtime whose package is not installed (there is nothing to check).
func (i *Installer) NotReady() []AssetStatus {
	var out []AssetStatus
	for _, s := range i.Assets() {
		if s.Required && s.State != AssetOK {
			out = append(out, s)
		}
	}
	return out
}

// AssetNeedBytes is the free space EnsureAssets needs for the assets whose files are not there: what is not downloaded yet, and for
// an archive its files as well. A file that is there is checked in place and needs nothing.
func (i *Installer) AssetNeedBytes() int64 {
	stamps := i.Store.Get().Files
	var need int64
	for _, a := range i.AssetSet.Assets {
		if i.assetState(a, stamps) != AssetMissing {
			continue
		}
		need += a.Size - partBytes(fetch.PartPath(i.assetDownloadPath(a)), a.Size)
		if a.IsArchive() {
			for _, m := range a.Members {
				need += m.Size
			}
		}
	}
	return need
}

func partBytes(part string, limit int64) int64 {
	if st, err := os.Stat(part); err == nil {
		return min(st.Size(), limit)
	}
	return 0
}

// assetDownloadPath is where the download of a lands: the final file for a single file (no second copy of a 700 MB weight), the
// downloads directory for an archive.
func (i *Installer) assetDownloadPath(a catalog.Resource) string {
	if a.IsArchive() {
		return filepath.Join(i.Layout.DownloadsDir(), path.Base(a.URL))
	}
	return i.envPath(a.Dest)
}

// CheckAssetSpace returns a *fetch.NoSpaceError when the disk cannot hold what is to download.
func (i *Installer) CheckAssetSpace() error {
	need := i.AssetNeedBytes()
	if need <= 0 {
		return nil
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

// EnsureAssets makes every asset of the catalog the pinned one. A file that is there with the right size is hashed (once: the record
// remembers the size and time of a verified file); one that is not the pinned file is deleted, and a missing one is downloaded from
// its pinned URL (resumed from its .part file, SHA-256 and size checked, the .part file deleted on a mismatch). What is verified is
// left alone, so a repeated call is cheap. The failure of an asset is an *AssetError; the assets before it stay done.
func (i *Installer) EnsureAssets(ctx context.Context, progress AssetProgress) error {
	stamps := i.Store.Get().Files
	type todo struct {
		a     catalog.Resource
		state AssetState
	}
	var work []todo
	var total int64
	for _, a := range i.AssetSet.Assets {
		if st := i.assetState(a, stamps); st != AssetOK {
			work = append(work, todo{a, st})
			total += a.Size
		}
	}
	if len(work) == 0 {
		return nil
	}
	if err := i.Layout.CheckWritable(); err != nil {
		return err
	}
	if err := i.CheckAssetSpace(); err != nil {
		return err
	}
	var done int64
	for _, w := range work {
		base := done
		report := func(ph Phase, d int64) {
			if progress != nil {
				progress(ph, w.a.Name, base+min(d, w.a.Size), total)
			}
		}
		if err := i.ensure(ctx, w.a, w.state, report); err != nil {
			return &AssetError{Name: w.a.Name, Err: err}
		}
		done += w.a.Size
	}
	if progress != nil {
		progress(PhaseWeights, "", total, total)
	}
	return nil
}
