package projectlink

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// ImageFile finds the image of a Project's asset, or of the Reference Library entry with the ID when reference is true, inside the assets directory.
func (l *Link) ImageFile(ctx context.Context, projectID, id string, reference bool) (path, mime string, err error) {
	assetID := id
	if reference {
		item, err := l.References.Get(ctx, projectID, id)
		if errors.Is(err, repository.ErrNotFound) {
			return "", "", gterr.Invalid("プロジェクトの参照画像 %q が見つかりません", id)
		}
		if err != nil {
			return "", "", err
		}
		assetID = item.AssetID
	}
	a, err := l.Assets.Get(ctx, projectID, assetID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", "", gterr.Invalid("プロジェクトの画像 %q が見つかりません", assetID)
	}
	if err != nil {
		return "", "", err
	}
	if a.Type == domain.AssetVideo || !strings.HasPrefix(a.MimeType, "image/") {
		return "", "", gterr.Invalid("プロジェクトの %q は画像ではありません", assetID)
	}
	path = filepath.Join(l.AssetsDir, filepath.FromSlash(a.FilePath))
	if rel, err := filepath.Rel(l.AssetsDir, path); err != nil || strings.HasPrefix(rel, "..") {
		return "", "", gterr.Invalid("プロジェクトの画像 %q の場所が正しくありません", assetID)
	}
	return path, a.MimeType, nil
}
