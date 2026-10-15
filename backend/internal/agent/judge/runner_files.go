package judge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// maxImageBytes bounds a stored image handed to a model.
const maxImageBytes = 20 << 20

type storedImage struct {
	data []byte
	mime string
	path string // the file, as in asset.file_path
}

// image reads the image of an asset. A missing or oversized file is skipped: a Reference Pack can
// be rebuilt, so a lost image must not stop the Judge.
func (r *Runner) image(ctx context.Context, projectID, assetID string) (storedImage, bool) {
	asset, err := r.Assets.Get(ctx, projectID, assetID)
	return r.read(assetID, asset, err)
}

// canonImage reads the image of a Series canon asset, which belongs to the Project the Series was
// made from, not to the Project being judged.
func (r *Runner) canonImage(ctx context.Context, assetID string) (storedImage, bool) {
	asset, err := r.Assets.GetAny(ctx, assetID)
	return r.read(assetID, asset, err)
}

// read reads the file of an asset that was looked up with err.
func (r *Runner) read(assetID string, asset domain.Asset, err error) (storedImage, bool) {
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			r.Logger.Warn("reference asset unavailable", "asset_id", assetID, "err", err)
		}
		return storedImage{}, false
	}
	abs, err := r.assetPath(asset.FilePath)
	if err != nil {
		r.Logger.Warn("reference asset path rejected", "asset_id", assetID, "err", err)
		return storedImage{}, false
	}
	if info, err := os.Stat(abs); err != nil || info.Size() > maxImageBytes {
		return storedImage{}, false
	}
	data, err := os.ReadFile(abs) // #nosec G304 -- inside the assets directory (assetPath)
	if err != nil {
		r.Logger.Warn("reference image unreadable", "asset_id", assetID, "err", err)
		return storedImage{}, false
	}
	return storedImage{data: data, mime: asset.MimeType, path: asset.FilePath}, true
}

// videoPath is the file of the Take.
func (r *Runner) videoPath(t domain.Take) (string, error) {
	if t.FilePath == "" {
		return "", fmt.Errorf("take %s has no video file", t.ID)
	}
	abs, err := r.assetPath(t.FilePath)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("take %s: %w", t.ID, err)
	}
	return abs, nil
}

// assetPath resolves a path stored in the database inside the assets directory.
func (r *Runner) assetPath(rel string) (string, error) {
	if r.AssetsDir == "" {
		return "", errors.New("assets directory is not configured")
	}
	abs := filepath.Join(r.AssetsDir, filepath.FromSlash(rel))
	if within, err := filepath.Rel(r.AssetsDir, abs); err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the assets directory", rel)
	}
	return abs, nil
}
