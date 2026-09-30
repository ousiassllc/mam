package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Bundle limits. The bundle is a local file the user chose, but a broken or hostile archive must
// not fill the disk or memory.
const (
	MaxBundleBytes     = 8 << 30 // the archive
	maxBundleFileBytes = 2 << 30 // one file in it
	// maxBundleExtractBytes is what an import writes to the assets directory at most, however much
	// room the disk has: the files the data names, added up.
	maxBundleExtractBytes = 32 << 30
	maxBundleDataBytes    = 512 << 20 // data.json and manifest.json
	maxBundleEntries      = 100_000
)

// BundleService exports a Project as a portable bundle and imports one
// (spec/requirements/non-functional.md §8). No cloud sync is involved: the bundle is one ZIP file
// with the Project Data Model as JSON and the asset files.
type BundleService struct {
	*core
	bundles   *repository.BundleRepository
	freeBytes func(path string) (uint64, error) // nil selects fsutil.FreeBytes
}

// BundleExport is a bundle ready to be written.
type BundleExport struct {
	// FileName is the suggested download name.
	FileName string
	data     domain.BundleData
	root     string
	now      time.Time
}

// Prepare reads the Project's data (ErrNotFound for an unknown Project). Write then streams the
// archive, so the caller can still answer with an error status before the first byte is sent.
func (s *BundleService) Prepare(ctx context.Context, projectID string) (BundleExport, error) {
	data, err := s.bundles.Export(ctx, projectID)
	if err != nil {
		return BundleExport{}, notFound(err)
	}
	now := time.Now().UTC()
	return BundleExport{
		FileName: fmt.Sprintf("mam-project-%s-%s.zip", now.Format("20060102"), projectID),
		data:     data, root: s.assetsDir, now: now,
	}, nil
}

// Write writes the bundle as a ZIP to w. Files that no longer exist (an archived Take) are left
// out; the data still lists them.
func (e BundleExport) Write(w io.Writer) error {
	zw := zip.NewWriter(w)
	manifest, err := json.Marshal(domain.BundleManifest{
		Format: domain.BundleFormat, Version: domain.BundleVersion, ProjectID: e.data.ProjectID, ExportedAt: e.now,
	})
	if err != nil {
		return err
	}
	if err := writeZipEntry(zw, domain.BundleManifestName, zip.Deflate, e.now, bytes.NewReader(manifest)); err != nil {
		return err
	}
	raw, err := json.Marshal(e.data)
	if err != nil {
		return err
	}
	if err := writeZipEntry(zw, domain.BundleDataName, zip.Deflate, e.now, bytes.NewReader(raw)); err != nil {
		return err
	}
	for _, rel := range e.data.FilePaths() {
		if !domain.ValidBundleFilePath(e.data.ProjectID, rel) || e.root == "" {
			continue
		}
		f, err := os.Open(filepath.Join(e.root, filepath.FromSlash(rel))) //nolint:gosec // rel was checked by ValidBundleFilePath
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		// Images and videos are compressed already.
		err = writeZipEntry(zw, domain.BundleAssetsDir+rel, zip.Store, e.now, f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeZipEntry(zw *zip.Writer, name string, method uint16, modified time.Time, r io.Reader) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: modified})
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// Import creates the Project of the bundle (POST /projects/import) and returns it. The Project
// keeps its ID and its files keep their paths, so a Project that already exists is ErrConflict and
// is never touched, not even by another import of the same bundle running at the same time. A file
// that is not a bundle of this format, or that holds data this version cannot import, is a
// ValidationError; nothing is left behind in either case.
func (s *BundleService) Import(ctx context.Context, src io.ReaderAt, size int64) (domain.Project, error) {
	if s.assetsDir == "" {
		return domain.Project{}, errors.New("import is not configured: assets directory is empty")
	}
	if size <= 0 || size > MaxBundleBytes {
		return domain.Project{}, invalid("bundle", "バンドルファイルのサイズが正しくありません（上限 %d GB）", MaxBundleBytes>>30)
	}
	zr, err := zip.NewReader(src, size)
	if err != nil || len(zr.File) > maxBundleEntries {
		return domain.Project{}, invalid("bundle", "プロジェクトバンドル（ZIP）として読み取れません")
	}
	data, err := readBundleData(zr)
	if err != nil {
		return domain.Project{}, err
	}
	timelinePaths, err := data.AudioTimelinePaths()
	if err != nil {
		return domain.Project{}, invalid("bundle", "音声タイムラインを読み取れません")
	}
	named := append(data.FilePaths(), timelinePaths...)
	for _, rel := range named {
		if !domain.ValidBundleFilePath(data.ProjectID, rel) {
			return domain.Project{}, invalid("bundle", "バンドルに不正なファイルパスがあります")
		}
	}
	files, err := bundleFiles(zr, data.ProjectID, named)
	if err != nil {
		return domain.Project{}, err
	}
	if err := s.checkRoom(files); err != nil {
		return domain.Project{}, err
	}
	if _, err := s.projects.Get(ctx, data.ProjectID); err == nil {
		return domain.Project{}, ErrConflict
	} else if !errors.Is(err, repository.ErrNotFound) {
		return domain.Project{}, err
	}

	err = s.writeProject(ctx, data, files)
	switch {
	case err == nil:
	case errors.Is(err, repository.ErrConflict):
		return domain.Project{}, ErrConflict
	case errors.Is(err, repository.ErrInvalidBundle):
		return domain.Project{}, invalid("bundle", "このバージョンでは読み込めないデータです: %s", strings.TrimPrefix(err.Error(), repository.ErrInvalidBundle.Error()+": "))
	default:
		return domain.Project{}, err
	}
	return s.getProject(ctx, data.ProjectID)
}

// readBundleData reads and checks manifest.json and data.json.
func readBundleData(zr *zip.Reader) (domain.BundleData, error) {
	var (
		manifest domain.BundleManifest
		data     domain.BundleData
	)
	if err := readZipJSON(zr, domain.BundleManifestName, &manifest, false); err != nil {
		return data, err
	}
	if manifest.Format != domain.BundleFormat {
		return data, invalid("bundle", "MAM のプロジェクトバンドルではありません")
	}
	if manifest.Version != domain.BundleVersion {
		return data, invalid("bundle", "対応していないバンドルのバージョンです（%d）", manifest.Version)
	}
	if err := readZipJSON(zr, domain.BundleDataName, &data, true); err != nil {
		return data, err
	}
	if data.ProjectID != manifest.ProjectID {
		return data, invalid("bundle", "manifest.json と data.json のプロジェクト ID が一致しません")
	}
	return data, nil
}

func readZipJSON(zr *zip.Reader, name string, v any, useNumber bool) error {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > maxBundleDataBytes {
			return invalid("bundle", "%s が大きすぎます", name)
		}
		rc, err := f.Open()
		if err != nil {
			return invalid("bundle", "%s を開けません", name)
		}
		defer func() { _ = rc.Close() }()
		dec := json.NewDecoder(io.LimitReader(rc, maxBundleDataBytes+1))
		if useNumber {
			dec.UseNumber()
		}
		if err := dec.Decode(v); err != nil {
			return invalid("bundle", "%s を読み取れません", name)
		}
		return nil
	}
	return invalid("bundle", "%s がありません", name)
}

// bundleFile is a file entry of the bundle with its asset-relative path.
type bundleFile struct {
	rel  string
	file *zip.File
	size int64 // the size the header declares, at most maxBundleFileBytes
}

// bundleFiles lists the asset entries of the archive that the data names (an entry nothing refers to
// is not extracted), refusing any entry whose path would leave the Project's directory, and any file
// that is over the size limit.
func bundleFiles(zr *zip.Reader, projectID string, named []string) ([]bundleFile, error) {
	wanted := make(map[string]bool, len(named))
	for _, rel := range named {
		wanted[rel] = true
	}
	var out []bundleFile
	for _, f := range zr.File {
		rel, ok := strings.CutPrefix(f.Name, domain.BundleAssetsDir)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		if !domain.ValidBundleFilePath(projectID, rel) || path.Clean(rel) != rel {
			return nil, invalid("bundle", "バンドルに不正なファイルパスがあります")
		}
		if !wanted[rel] {
			continue
		}
		if f.UncompressedSize64 > maxBundleFileBytes {
			return nil, invalid("bundle", "%s が大きすぎます", rel)
		}
		delete(wanted, rel)                                                                 // a name that appears twice is extracted once
		out = append(out, bundleFile{rel: rel, file: f, size: int64(f.UncompressedSize64)}) //nolint:gosec // checked against maxBundleFileBytes above
	}
	return out, nil
}

// checkRoom refuses files that add up to more than an import may write, or more than the disk that
// holds the assets directory has free. Nothing has been written yet.
func (s *BundleService) checkRoom(files []bundleFile) error {
	var total uint64
	for _, bf := range files {
		total += bf.file.UncompressedSize64
	}
	if total > maxBundleExtractBytes {
		return invalid("bundle", "バンドルのファイルの合計が大きすぎます（上限 %d GB）", int64(maxBundleExtractBytes)>>30)
	}
	freeBytes := s.freeBytes
	if freeBytes == nil {
		freeBytes = fsutil.FreeBytes
	}
	if free, err := freeBytes(s.assetsDir); err == nil && total > free {
		return invalid("bundle", "ディスクの空き容量が足りません（必要 %s、空き %s）", domain.FormatBytes(total), domain.FormatBytes(free))
	}
	return nil
}

// writeProject puts the Project of the bundle in place. The files are extracted into a staging
// directory first; moving the Project's directory into the assets directory is what claims it, so of
// two imports of the same Project only one gets there (the other is ErrConflict and removes nothing
// but its own staging directory). A database failure removes the directory this import moved.
func (s *BundleService) writeProject(ctx context.Context, data domain.BundleData, files []bundleFile) error {
	var moved string
	if len(files) > 0 {
		staging := filepath.Join(s.assetsDir, ".import-"+uuid.NewString())
		defer func() { _ = os.RemoveAll(staging) }()
		if err := extractFiles(staging, files); err != nil {
			return err
		}
		target := filepath.Join(s.assetsDir, data.ProjectID)
		if err := os.Rename(filepath.Join(staging, data.ProjectID), target); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return repository.ErrConflict
			}
			return fmt.Errorf("move imported files: %w", err)
		}
		moved = target
	}
	if err := s.bundles.Import(ctx, data); err != nil {
		if moved != "" {
			_ = os.RemoveAll(moved)
		}
		return err
	}
	return nil
}

// extractFiles writes the files below root (the asset-relative paths are kept).
func extractFiles(root string, files []bundleFile) error {
	for _, bf := range files {
		abs := filepath.Join(root, filepath.FromSlash(bf.rel))
		if err := os.MkdirAll(filepath.Dir(abs), assetDirPerm); err != nil {
			return fmt.Errorf("create asset directory: %w", err)
		}
		err := fsutil.WriteAtomic(abs, assetFilePerm, func(w io.Writer) error {
			rc, err := bf.file.Open()
			if err != nil {
				return err
			}
			defer func() { _ = rc.Close() }()
			// The header's size is what checkRoom counted; an entry that holds more is refused.
			n, err := io.Copy(w, io.LimitReader(rc, bf.size+1))
			if err == nil && n > bf.size {
				err = invalid("bundle", "%s が大きすぎます", bf.rel)
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
