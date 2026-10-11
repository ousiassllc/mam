package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/bundlezip"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/bundle"
)

// MaxBundleBytes is the largest bundle an import accepts.
const MaxBundleBytes = bundlezip.MaxBytes

// BundleService exports a Project as a portable bundle and imports one
// (spec/requirements/non-functional.md §8). No cloud sync is involved: the bundle is one ZIP file
// with the Project Data Model as JSON and the asset files (the ZIP itself is bundlezip).
type BundleService struct {
	*core
	bundles   *bundle.Repository
	freeBytes func(path string) (uint64, error) // nil selects fsutil.FreeBytes
}

// BundleExport is a bundle ready to be written.
type BundleExport struct {
	// FileName is the suggested download name.
	FileName string
	bundlezip.Export
}

// Prepare reads the Project's data (ErrNotFound for an unknown Project). Write then streams the
// archive, so the caller can still answer with an error status before the first byte is sent.
func (s *BundleService) Prepare(ctx context.Context, projectID string) (BundleExport, error) {
	data, err := s.bundles.Export(ctx, projectID)
	if err != nil {
		return BundleExport{}, notFound(err)
	}
	now := time.Now().UTC()
	exp, err := bundlezip.NewExport(data, s.assetsDir, now)
	if err != nil {
		return BundleExport{}, err
	}
	return BundleExport{
		FileName: fmt.Sprintf("mam-project-%s-%s.zip", now.Format("20060102"), projectID),
		Export:   exp,
	}, nil
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
	archive, err := bundlezip.Open(src, size)
	if err != nil {
		return domain.Project{}, bundleInvalid(err)
	}
	if err := archive.CheckRoom(s.assetsDir, s.freeBytes); err != nil {
		return domain.Project{}, bundleInvalid(err)
	}
	data := archive.Data
	if _, err := s.projects.Get(ctx, data.ProjectID); err == nil {
		return domain.Project{}, ErrConflict
	} else if !errors.Is(err, repository.ErrNotFound) {
		return domain.Project{}, err
	}
	err = s.writeProject(ctx, archive)
	switch {
	case err == nil:
	case errors.Is(err, repository.ErrConflict), errors.Is(err, bundlezip.ErrProjectDirExists):
		return domain.Project{}, ErrConflict
	case errors.Is(err, bundle.ErrInvalid):
		return domain.Project{}, invalid("bundle", "このバージョンでは読み込めないデータです: %s", strings.TrimPrefix(err.Error(), bundle.ErrInvalid.Error()+": "))
	default:
		return domain.Project{}, bundleInvalid(err)
	}
	return s.getProject(ctx, data.ProjectID)
}

// bundleInvalid turns the error of an archive that cannot be imported into a ValidationError and
// returns any other error as it is.
func bundleInvalid(err error) error {
	var bad *bundlezip.InvalidError
	if errors.As(err, &bad) {
		return invalid("bundle", "%s", bad.Message)
	}
	return err
}

// writeProject puts the Project of the bundle in place: its files first, then its rows. A database
// failure removes the directory this import created.
func (s *BundleService) writeProject(ctx context.Context, archive *bundlezip.Archive) error {
	moved, err := archive.Install(s.assetsDir)
	if err != nil {
		return err
	}
	if err := s.bundles.Import(ctx, archive.Data); err != nil {
		if moved != "" {
			_ = os.RemoveAll(moved)
		}
		return err
	}
	return nil
}
