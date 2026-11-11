package service

import (
	"context"
	"errors"
	"os"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Storage returns the disk usage of the Project, the size of the preview directory and the free space of the assets volume.
func (s *PolicyService) Storage(ctx context.Context, projectID string) (StorageView, error) {
	sum, err := s.storage.Usage(ctx, projectID)
	if err != nil {
		return StorageView{}, notFound(err)
	}
	freeBytes := s.freeBytes
	if freeBytes == nil {
		freeBytes = fsutil.FreeBytes
	}
	if free, err := freeBytes(s.assetsDir); err == nil {
		sum.FreeBytes, sum.FreeKnown = free, true
	} else {
		s.logger.Warn("read free disk space failed", "err", err)
	}
	if sum.PreviewBytes, err = s.previews.Size(); err != nil {
		s.logger.Warn("measure the preview directory failed", "err", err)
	}
	return StorageView{ProjectID: projectID, StorageSummary: sum}, nil
}

// ArchiveTake gives up the video file of one Take that is not the Selected Take of its Shot. The
// Take stays as Rejected with its history. ErrConflict is returned for a Selected Take and for a Take
// a Timeline still refers to (its file is needed to export).
func (s *PolicyService) ArchiveTake(ctx context.Context, projectID, takeID string) (StorageView, error) {
	removed, err := s.storage.ArchiveTake(ctx, projectID, takeID)
	if errors.Is(err, repository.ErrConflict) {
		return StorageView{}, conflict("この Take は選択中か、編集・音声のタイムラインで使われているためアーカイブできません")
	}
	if err != nil {
		return StorageView{}, notFound(err)
	}
	s.removeFiles(removed, takeID)
	return s.Storage(ctx, projectID)
}

// ArchiveUnneeded archives every unneeded Take of the Project: the Rejected ones and the ones that
// were not chosen where another Take of the Shot is Selected.
func (s *PolicyService) ArchiveUnneeded(ctx context.Context, projectID string) (StorageView, error) {
	before, err := s.storage.Usage(ctx, projectID) // the Takes to be freed, for their previews
	if err != nil {
		return StorageView{}, notFound(err)
	}
	removed, err := s.storage.ArchiveUnneeded(ctx, projectID)
	if err != nil {
		return StorageView{}, notFound(err)
	}
	var takeIDs []string
	for _, t := range before.Takes {
		if t.Unneeded {
			takeIDs = append(takeIDs, t.TakeID)
		}
	}
	s.removeFiles(removed, takeIDs...)
	return s.Storage(ctx, projectID)
}

// removeFiles deletes the files of archived assets and the previews (proxy, thumbnail) of the
// archived Takes. The rows are already gone, so a failure only leaves an orphan file: logged for a
// Take file, deleted by the size limit in time for a preview.
func (s *PolicyService) removeFiles(assets []domain.Asset, takeIDs ...string) {
	for _, a := range assets {
		path := s.assetPath(a.FilePath)
		if path == "" {
			s.logger.Error("skip archived take file outside the assets directory", "asset_id", a.ID, "file_path", a.FilePath)
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logger.Error("remove archived take file failed", "asset_id", a.ID, "err", err)
		}
	}
	for _, id := range takeIDs {
		for _, ext := range []string{".mp4", ".jpg"} {
			_ = os.Remove(s.previews.Path(id + ext)) // "" (an unsafe ID) and a preview that was never made are not there
		}
	}
}
