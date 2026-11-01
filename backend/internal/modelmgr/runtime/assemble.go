package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
)

// assemble puts the downloaded parts of a build together as the environment in tmp: each part is extracted in the order of the
// catalog (the embeddable Python, the vendor's source, the wheels), then the files of the build are written (the path file of the
// Python and mam's own server script). Nothing is run: no pip, no setup script, no network. Each part was checked against its SHA-256
// by the download, and its extraction is held to the size the catalog pinned for it.
func (i *Installer) assemble(ctx context.Context, a catalog.RuntimeVariant, files []packageFile, tmp string, progress Progress) error {
	b := i.Runtime.Build
	if b == nil {
		return fmt.Errorf("runtime: %s has parts but no build", i.Runtime.ID)
	}
	var base int64
	for n, p := range a.Parts {
		report := func(done, _ int64) {
			if progress != nil {
				progress(PhaseExtract, base+done, a.ExtractedSize)
			}
		}
		var err error
		switch p.Kind {
		case parts.KindZip:
			err = extract.Zip(ctx, files[n].Path, filepath.Join(tmp, filepath.FromSlash(p.Dest)), p.Strip, p.ExtractedSize, report)
		case parts.KindWheel:
			err = extract.Wheel(ctx, files[n].Path, filepath.Join(tmp, filepath.FromSlash(b.SitePackages)), p.ExtractedSize, report)
		default:
			err = fmt.Errorf("runtime: part %s has the kind %q", p.Name, p.Kind)
		}
		if err != nil {
			return fmt.Errorf("runtime: extract %s: %w", p.Name, err)
		}
		base += p.ExtractedSize
	}
	if err := i.writeFiles(tmp, b.Files); err != nil {
		return err
	}
	if progress != nil {
		progress(PhaseExtract, a.ExtractedSize, a.ExtractedSize)
	}
	return nil
}

// writeFiles writes the files mam owns (the path file of the Python, mam's own server and launcher scripts) into the environment in
// tmp; a file of the same name that the archive holds is replaced, so the file mam wrote is the one that runs.
func (i *Installer) writeFiles(tmp string, files []parts.File) error {
	for _, f := range files {
		data := []byte(strings.Join(f.Lines, "\n") + "\n")
		if f.Overlay != "" {
			var ok bool
			if data, ok = i.Overlays[f.Overlay]; !ok {
				return fmt.Errorf("runtime: the build has no overlay %q", f.Overlay)
			}
		}
		target := filepath.Join(tmp, filepath.FromSlash(f.Dest))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("runtime: write %s: %w", f.Dest, err)
		}
		if err := os.WriteFile(target, data, 0o640); err != nil { //nolint:gosec // a file of the runtime's own environment, readable by its interpreter
			return fmt.Errorf("runtime: write %s: %w", f.Dest, err)
		}
	}
	return nil
}
