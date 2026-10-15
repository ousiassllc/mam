package bundlezip

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/bundlepath"
)

// Export is a bundle ready to be written.
type Export struct {
	data domain.BundleData
	root string
	now  time.Time
	// sources maps a bundle path that does not exist on disk under that name to the file it is read
	// from: an Episode's copy of a canon image shares the file of the Series' first Project.
	sources map[string]string
}

// NewExport prepares the bundle of data, whose files are read from below root when Write runs.
// A file that cannot be written under assets/<project>/ would make a bundle that does not import:
// it refuses to export it rather than leave the file out without a word.
func NewExport(data domain.BundleData, root string, now time.Time) (Export, error) {
	sources := relocateSharedAssets(&data)
	timelinePaths, err := data.AudioTimelinePaths()
	if err != nil {
		return Export{}, fmt.Errorf("audio timeline: %w", err)
	}
	for _, rel := range append(data.FilePaths(), timelinePaths...) {
		if !bundlepath.ValidFilePath(data.ProjectID, rel) {
			return Export{}, fmt.Errorf("export bundle: %q is not a file path of the project", rel)
		}
	}
	return Export{data: data, root: root, now: now, sources: sources}, nil
}

// relocateSharedAssets gives the asset rows whose file lives in another Project's directory (the
// canon images an Episode of a Series shares with the Series' first Project) a path of the exported
// Project: <project>/<asset id><extension>. It returns the new path to the original one, for the
// files to be read from there.
func relocateSharedAssets(data *domain.BundleData) map[string]string {
	sources := map[string]string{}
	for _, t := range data.Tables {
		if t.Name != "asset" {
			continue
		}
		for _, row := range t.Rows {
			old, _ := row["file_path"].(string)
			id, _ := row["id"].(string)
			owner, _, _ := strings.Cut(old, "/")
			if old == "" || owner == data.ProjectID || !bundlepath.ValidFilePath(owner, old) || !bundlepath.ValidID(id) {
				continue
			}
			moved := data.ProjectID + "/" + id + path.Ext(old)
			row["file_path"] = moved
			sources[moved] = old
		}
	}
	return sources
}

// Write writes the bundle as a ZIP to w. Files that no longer exist (an archived Take) are left
// out; the data still lists them.
func (e Export) Write(w io.Writer) error {
	zw := zip.NewWriter(w)
	manifest := domain.BundleManifest{
		Format: domain.BundleFormat, Version: domain.BundleVersion, ProjectID: e.data.ProjectID, ExportedAt: e.now,
	}
	if err := WriteJSON(zw, domain.BundleManifestName, e.now, manifest); err != nil {
		return err
	}
	if err := WriteJSON(zw, domain.BundleDataName, e.now, e.data); err != nil {
		return err
	}
	timelinePaths, err := e.data.AudioTimelinePaths()
	if err != nil {
		return fmt.Errorf("audio timeline: %w", err)
	}
	// The mix, clips and Lip Sync pictures live in the Audio Timeline JSON only; without them an
	// imported Project cannot be exported to video.
	seen := map[string]bool{}
	for _, rel := range append(e.data.FilePaths(), timelinePaths...) {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if !bundlepath.ValidFilePath(e.data.ProjectID, rel) || e.root == "" {
			continue
		}
		from := rel
		if src, ok := e.sources[rel]; ok {
			from = src
		}
		f, err := os.Open(filepath.Join(e.root, filepath.FromSlash(from))) //nolint:gosec // from was checked by bundlepath.ValidFilePath
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		// Images and videos are compressed already.
		err = WriteEntry(zw, domain.BundleAssetsDir+rel, zip.Store, e.now, f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

// WriteJSON adds v to zw as a compressed JSON file (the review package of service uses it too).
func WriteJSON(zw *zip.Writer, name string, modified time.Time, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteEntry(zw, name, zip.Deflate, modified, bytes.NewReader(raw))
}

// WriteEntry adds one file to zw.
func WriteEntry(zw *zip.Writer, name string, method uint16, modified time.Time, r io.Reader) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: modified})
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
