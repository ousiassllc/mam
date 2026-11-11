package chatwire

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	return ownEntry(ctx, library, projectID, newEntries(seen, view.Items, kind, strings.TrimSpace(name)), abs)
}

// newEntries are the entries of the Library that were not there before the upload and are of the kind and the name the upload gave: Upload returns the whole Library, not the entry,
// and another upload (a second chat, the Library screen) may land at the same time, so "the one that is new" is not enough to tell the entry of this upload.
func newEntries(seen map[string]bool, items []domain.ReferenceItem, kind domain.ReferenceKind, name string) []domain.ReferenceItem {
	var out []domain.ReferenceItem
	for _, it := range items {
		if !seen[it.ID] && it.Kind == kind && it.Name == name && it.Source == domain.SourceUploaded {
			out = append(out, it)
		}
	}
	return out
}

// ownEntry is the entry of this upload among the candidates: the only one, or (when two uploads of the same kind and name met) the one that holds the file that was uploaded.
func ownEntry(ctx context.Context, library *service.ReferenceService, projectID string, candidates []domain.ReferenceItem, srcPath string) (domain.ReferenceItem, error) {
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	want, err := os.ReadFile(srcPath) //nolint:gosec // srcPath is inside the assets directory (StoredAsset)
	if err != nil {
		return domain.ReferenceItem{}, service.ErrNotFound
	}
	for _, it := range candidates {
		if _, abs, err := library.StoredAsset(ctx, projectID, it.AssetID); err == nil {
			if got, err := os.ReadFile(abs); err == nil && bytes.Equal(got, want) { //nolint:gosec // abs is inside the assets directory (StoredAsset)
				return it, nil
			}
		}
	}
	return domain.ReferenceItem{}, service.ErrNotFound
}
