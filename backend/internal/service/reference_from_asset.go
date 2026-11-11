package service

import (
	"context"
	"os"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// StoredAsset is an asset of the Project (an attachment of the chat) and where its file is on disk: what the chat needs to register an attachment in the Library through
// Upload, so that the file passes the checks of a file picked on the Library screen (spec/components/chat-settings-operations.md §6). ErrNotFound when the asset or its file is gone.
func (s *ReferenceService) StoredAsset(ctx context.Context, projectID, assetID string) (domain.Asset, string, error) {
	asset, err := s.assets.Get(ctx, projectID, assetID)
	if err != nil {
		return domain.Asset{}, "", notFound(err)
	}
	abs := s.assetPath(asset.FilePath)
	if info, err := os.Stat(abs); abs == "" || err != nil || !info.Mode().IsRegular() {
		return domain.Asset{}, "", ErrNotFound
	}
	return asset, abs, nil
}
