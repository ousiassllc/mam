package chatwire

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// addFromAsset registers an image the Project already stores (an attachment of the chat: an asset of type image) in the Reference Library as a new entry of the kind (source
// uploaded) and returns the entry. The file is copied through Upload, so that it passes the same checks as a file picked on the Library screen (kind, name, image type and
// content, size) and the entry owns an asset of type reference of its own: removing the entry or the attachment never touches the other. Nothing is registered when a check fails.
func addFromAsset(ctx context.Context, library *service.ReferenceService, projectID, assetID string, kind domain.ReferenceKind, name string) (domain.ReferenceItem, error) {
	asset, abs, err := library.StoredAsset(ctx, projectID, assetID)
	if err != nil {
		return domain.ReferenceItem{}, err
	}
	if asset.Type != domain.AssetImage {
		return domain.ReferenceItem{}, &service.ValidationError{Field: "asset", Message: "画像（jpg/png/webp/gif）を選んでください"}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return domain.ReferenceItem{}, service.ErrNotFound
	}
	known, err := library.Library(ctx, projectID, service.LibraryQuery{})
	if err != nil {
		return domain.ReferenceItem{}, err
	}
	seen := make(map[string]bool, len(known.Items))
	for _, it := range known.Items {
		seen[it.ID] = true
	}
	view, err := library.Upload(ctx, projectID, service.UploadReferenceInput{
		File: service.Upload{Name: filepath.Base(asset.FilePath), Size: info.Size(), Open: func() (io.ReadCloser, error) { return os.Open(abs) }}, //nolint:gosec // abs is inside the assets directory (StoredAsset)
		Kind: string(kind), Name: name,
	})
	if err != nil {
		return domain.ReferenceItem{}, err
	}
	for _, it := range view.Items {
		if !seen[it.ID] {
			return it, nil
		}
	}
	return domain.ReferenceItem{}, service.ErrNotFound
}
