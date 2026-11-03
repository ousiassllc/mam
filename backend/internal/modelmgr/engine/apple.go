package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// The macOS (Apple silicon) engine (spec/architecture/model-manager/model-manager.md §3.7). ComfyUI ships a portable package for Windows
// only, so there is no archive to fetch: mam downloads the pinned parts (the relocatable Python of python-build-standalone, the ComfyUI source
// of the pinned commit, and the Python wheels of its requirements with PyTorch's Metal backend), checks each against its SHA-256 and size,
// and extracts them into the engine directory itself. Nothing is run: no pip, no setup script, and the engine fetches nothing at its start.

// errNoApple is the answer to an install of the macOS build when the catalog has none.
var errNoApple = fmt.Errorf("%w: the catalog has no macOS build", ErrVariant)

// appleFile is a part of the macOS build that is downloaded: where it is kept and what it must be.
type appleFile struct {
	Path string
	parts.Part
}

// appleFiles are the files the install downloads, in the order they are extracted. The index in the name keeps two parts that share a name
// (never in a valid catalog) from sharing a file.
func (i *Installer) appleFiles(a *catalog.AppleEngine) []appleFile {
	files := make([]appleFile, 0, len(a.Parts))
	for n, p := range a.Parts {
		files = append(files, appleFile{Path: filepath.Join(i.Layout.DownloadsDir(), fmt.Sprintf("%02d-%s.%s", n, p.Name, partExt(p.Kind))), Part: p})
	}
	return files
}

func partExt(kind string) string {
	if kind == parts.KindTarGz {
		return "tar.gz"
	}
	return "zip" // a zip, and a wheel (which is one)
}

// appleNeedBytes is NeedBytes of the macOS build: what of the parts is not downloaded yet (a part that was downloaded whole and kept by an
// interrupted install counts as done; it is verified again before it is used), and the extracted files.
func (i *Installer) appleNeedBytes() (int64, error) {
	a := i.Engine.Apple
	if a == nil {
		return 0, errNoApple
	}
	need := a.ExtractedSize
	for _, f := range i.appleFiles(a) {
		have := int64(0)
		if st, err := os.Stat(f.Path); err == nil && st.Size() == f.Size {
			have = f.Size
		} else if st, err := os.Stat(fetch.PartPath(f.Path)); err == nil {
			have = min(st.Size(), f.Size)
		}
		need += f.Size - have
	}
	return need, nil
}

// installApple is Install of the macOS build: download every part (resumed from its .part file, checked against its SHA-256 and size),
// assemble them in engine.new, swap it in for the installed engine and record it. A failure removes the partial assembly; the downloads stay
// for a resume (unless a checksum was wrong, which removes that file).
func (i *Installer) installApple(ctx context.Context, cpu bool, progress Progress) error {
	a := i.Engine.Apple
	if a == nil {
		return errNoApple
	}
	if err := i.Layout.CheckWritable(); err != nil {
		return err
	}
	if err := i.CheckSpace(catalog.VariantApple); err != nil {
		return err
	}
	files := i.appleFiles(a)
	if err := i.downloadParts(ctx, files, a.Size, progress); err != nil {
		return err
	}
	tmp := i.Layout.EngineNewDir()
	_ = os.RemoveAll(tmp)
	err := i.assembleApple(ctx, a, files, tmp, progress)
	if err == nil {
		err = i.swap(tmp, a.Python, a.Main)
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := i.record(catalog.VariantApple, cpu); err != nil {
		return err
	}
	for _, f := range files {
		_ = os.Remove(f.Path)
	}
	_ = os.Remove(i.Layout.DownloadsDir()) // only when empty
	return nil
}

// downloadParts fetches the files one after the other, reporting the bytes of all of them against total. The parts come from more hosts
// than the Windows release asset (python-build-standalone on GitHub, PyPI's files.pythonhosted.org); every one is pinned by SHA-256 and
// size, so the host is where the bytes are fetched from and not what they are trusted by.
func (i *Installer) downloadParts(ctx context.Context, files []appleFile, total int64, progress Progress) error {
	opts := i.Fetch
	if opts.CheckURL == nil {
		opts.CheckURL = fetch.PackageURL
	}
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

// assembleApple puts the downloaded parts together as the engine in tmp, in the order of the catalog (the Python, the source, the wheels).
// Each extraction is held to the size the catalog pinned for it, writes nothing outside tmp and never overwrites a file another part wrote.
func (i *Installer) assembleApple(ctx context.Context, a *catalog.AppleEngine, files []appleFile, tmp string, progress Progress) error {
	var base int64
	for _, f := range files {
		report := func(done, _ int64) {
			if progress != nil {
				progress(PhaseExtract, base+done, a.ExtractedSize)
			}
		}
		dest := filepath.Join(tmp, filepath.FromSlash(f.Dest))
		var err error
		switch f.Kind {
		case parts.KindTarGz:
			err = extract.TarGz(ctx, f.Path, dest, f.Strip, f.ExtractedSize, report)
		case parts.KindZip:
			err = extract.Zip(ctx, f.Path, dest, f.Strip, f.ExtractedSize, report)
		case parts.KindWheel:
			err = extract.Wheel(ctx, f.Path, filepath.Join(tmp, filepath.FromSlash(a.SitePackages)), f.ExtractedSize, report)
		default:
			err = errors.New("unknown kind " + f.Kind)
		}
		if err != nil {
			return fmt.Errorf("engine: extract %s: %w", f.Name, err)
		}
		base += f.ExtractedSize
	}
	if progress != nil {
		progress(PhaseExtract, a.ExtractedSize, a.ExtractedSize)
	}
	return nil
}
