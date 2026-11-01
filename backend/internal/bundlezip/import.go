package bundlezip

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/bundlepath"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// ErrProjectDirExists is returned by Install when the Project's directory is already in the assets
// directory.
var ErrProjectDirExists = errors.New("the project's asset directory already exists")

// Archive is an opened bundle whose manifest and data are read and checked. Nothing is written to
// disk until Install.
type Archive struct {
	// Data is the Project Data Model of the bundle.
	Data  domain.BundleData
	files []file
	total uint64 // what Install writes, added up
}

// file is a file entry of the bundle with its asset-relative path.
type file struct {
	rel  string
	file *zip.File
	size int64 // the size the header declares, at most maxFileBytes
}

// Open reads and checks manifest.json and data.json of the bundle in src (size bytes) and lists the
// asset files the data names; an entry nothing refers to is not extracted. A file that is not a
// bundle of this format, or whose paths or sizes are not acceptable, is an *InvalidError.
func Open(src io.ReaderAt, size int64) (*Archive, error) {
	if size <= 0 || size > MaxBytes {
		return nil, invalid("バンドルファイルのサイズが正しくありません（上限 %d GB）", int64(MaxBytes)>>30)
	}
	zr, err := zip.NewReader(src, size)
	if err != nil || len(zr.File) > maxEntries {
		return nil, invalid("プロジェクトバンドル（ZIP）として読み取れません")
	}
	data, err := readData(zr)
	if err != nil {
		return nil, err
	}
	named, err := dataPaths(data)
	if err != nil {
		return nil, invalid("音声タイムラインまたは参照音声を読み取れません")
	}
	for _, rel := range named {
		if !bundlepath.ValidFilePath(data.ProjectID, rel) {
			return nil, invalid("バンドルに不正なファイルパスがあります")
		}
	}
	if bundlepath.Conflict(named) {
		return nil, invalid("バンドルに同じ名前のファイル（大文字小文字の違いだけのもの、またはファイルとフォルダ）が複数あります")
	}
	files, err := listFiles(zr, data.ProjectID, named)
	if err != nil {
		return nil, err
	}
	a := &Archive{Data: data, files: files}
	for _, f := range files {
		a.total += f.file.UncompressedSize64
	}
	return a, nil
}

// readData reads and checks manifest.json and data.json.
func readData(zr *zip.Reader) (domain.BundleData, error) {
	var (
		manifest domain.BundleManifest
		data     domain.BundleData
	)
	err := readJSON(zr, domain.BundleManifestName, maxManifest, func(r io.Reader) error {
		return json.NewDecoder(r).Decode(&manifest)
	})
	if err != nil {
		return data, err
	}
	if manifest.Format != domain.BundleFormat {
		return data, invalid("MAM のプロジェクトバンドルではありません")
	}
	if manifest.Version != domain.BundleVersion {
		return data, invalid("対応していないバンドルのバージョンです（%d）", manifest.Version)
	}
	err = readJSON(zr, domain.BundleDataName, maxDataBytes, func(r io.Reader) (err error) {
		data, err = decodeData(r)
		return err
	})
	if errors.Is(err, errDataTooLarge) {
		return data, invalid("%s の行数・列数・入れ子の深さが上限を超えています", domain.BundleDataName)
	}
	if err != nil {
		return data, err
	}
	if data.ProjectID != manifest.ProjectID {
		return data, invalid("manifest.json と data.json のプロジェクト ID が一致しません")
	}
	// Install puts the files in assets/<project_id>/ before the rows are checked, so a Project ID the
	// server uses for its own directory (the Review inbox) is refused here, before anything is written.
	if !bundlepath.ValidID(data.ProjectID) {
		return data, invalid("プロジェクト ID が正しくありません")
	}
	return data, nil
}

// readJSON runs decode on the entry name, whose size must be at most limit bytes (the declared size
// and what is read are both held to it). decode errors are an *InvalidError ("cannot read"), except
// the ones that are one already or errDataTooLarge.
func readJSON(zr *zip.Reader, name string, limit int64, decode func(io.Reader) error) error {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > uint64(limit) { //nolint:gosec // limit is a positive constant
			return invalid("%s が大きすぎます", name)
		}
		rc, err := f.Open()
		if err != nil {
			return invalid("%s を開けません", name)
		}
		defer func() { _ = rc.Close() }()
		if err := decode(io.LimitReader(rc, limit+1)); err != nil {
			if errors.Is(err, errDataTooLarge) {
				return err
			}
			return invalid("%s を読み取れません", name)
		}
		return nil
	}
	return invalid("%s がありません", name)
}

// listFiles lists the asset entries of the archive that the data names, refusing any entry whose
// path would leave the Project's directory, and any file that is over the size limit.
func listFiles(zr *zip.Reader, projectID string, named []string) ([]file, error) {
	wanted := make(map[string]bool, len(named))
	for _, rel := range named {
		wanted[rel] = true
	}
	var out []file
	for _, f := range zr.File {
		rel, ok := strings.CutPrefix(f.Name, domain.BundleAssetsDir)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		if !bundlepath.ValidFilePath(projectID, rel) || path.Clean(rel) != rel {
			return nil, invalid("バンドルに不正なファイルパスがあります")
		}
		if !wanted[rel] {
			continue
		}
		if f.UncompressedSize64 > maxFileBytes {
			return nil, invalid("%s が大きすぎます", rel)
		}
		delete(wanted, rel)                                                           // a name that appears twice is extracted once
		out = append(out, file{rel: rel, file: f, size: int64(f.UncompressedSize64)}) //nolint:gosec // checked against maxFileBytes above
	}
	return out, nil
}

// CheckRoom refuses files that add up to more than an import may write, or more than the disk that
// holds assetsDir has free (freeBytes reports it; nil selects fsutil.FreeBytes). Nothing has been
// written yet.
func (a *Archive) CheckRoom(assetsDir string, freeBytes func(path string) (uint64, error)) error {
	if a.total > maxExtractBytes {
		return invalid("バンドルのファイルの合計が大きすぎます（上限 %d GB）", int64(maxExtractBytes)>>30)
	}
	if freeBytes == nil {
		freeBytes = fsutil.FreeBytes
	}
	if free, err := freeBytes(assetsDir); err == nil && a.total > free {
		return invalid("ディスクの空き容量が足りません（必要 %s、空き %s）", domain.FormatBytes(a.total), domain.FormatBytes(free))
	}
	return nil
}

// importStagingPrefix starts the name of the directory an import extracts into, inside the assets directory.
const importStagingPrefix = ".import-"

// RemoveStaleImports deletes the extraction directories (assets/.import-*) an import that was killed
// halfway left behind, and returns their paths. Call it at startup, when no import can be running.
//
// It is best effort: a directory that cannot be removed does not stop the others from being removed.
// The returned error joins every failure (nil when there was none); the paths returned are those removed.
func RemoveStaleImports(assetsDir string) ([]string, error) {
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	var failures []error
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), importStagingPrefix) {
			continue
		}
		dir := filepath.Join(assetsDir, e.Name())
		if err := os.RemoveAll(dir); err != nil {
			failures = append(failures, err)
			continue
		}
		removed = append(removed, dir)
	}
	return removed, errors.Join(failures...)
}

// Install puts the files of the Project into assetsDir/<project>. They are extracted into a staging
// directory first; moving the Project's directory into place is what claims it, so of two imports of
// the same Project only one gets there (the other gets ErrProjectDirExists and removes nothing but
// its own staging directory). It returns the directory it created ("" when the bundle has no
// files), for the caller to remove with os.RemoveAll if what follows fails.
func (a *Archive) Install(assetsDir string) (string, error) {
	if len(a.files) == 0 {
		return "", nil
	}
	staging := filepath.Join(assetsDir, importStagingPrefix+uuid.NewString())
	defer func() { _ = os.RemoveAll(staging) }()
	if err := extractFiles(staging, a.files); err != nil {
		return "", err
	}
	target := filepath.Join(assetsDir, a.Data.ProjectID)
	if err := renameDir(filepath.Join(staging, a.Data.ProjectID), target); err != nil {
		// Moving onto an existing directory fails as fs.ErrExist on Linux but as access denied
		// (fs.ErrPermission) on Windows, so the error kind does not say who lost the race: a target
		// that is there now does.
		if errors.Is(err, fs.ErrExist) || targetExists(target) {
			return "", ErrProjectDirExists
		}
		return "", fmt.Errorf("move imported files: %w", err)
	}
	return target, nil
}

// renameDir moves the extracted Project directory into place; a variable so that a test can make it
// fail the way Windows does.
var renameDir = os.Rename

// targetExists reports whether something is at path.
func targetExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// extractFiles writes the files below root (the asset-relative paths are kept).
func extractFiles(root string, files []file) error {
	for _, f := range files {
		abs := filepath.Join(root, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(abs), dirPerm); err != nil {
			return fmt.Errorf("create asset directory: %w", err)
		}
		err := fsutil.WriteAtomic(abs, filePerm, func(w io.Writer) error {
			rc, err := f.file.Open()
			if err != nil {
				return err
			}
			defer func() { _ = rc.Close() }()
			// The header's size is what CheckRoom counted; an entry that holds more is refused.
			n, err := io.Copy(w, io.LimitReader(rc, f.size+1))
			if err == nil && n > f.size {
				err = invalid("%s が大きすぎます", f.rel)
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
