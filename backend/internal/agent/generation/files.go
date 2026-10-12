package generation

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
)

const (
	assetDirPerm  = 0o700
	assetFilePerm = 0o600
	maxInputBytes = 20 << 20 // bounds a stored image handed to a model as input
)

// fileStore reads and writes the files of assets under the assets directory; media, when set, probes them.
type fileStore struct {
	root  string
	media *media.Tools
}

var fileExts = map[string]string{"image/jpeg": ".jpg", "image/webp": ".webp", "video/webm": ".webm", "video/quicktime": ".mov", "video/mp4": ".mp4", "image/png": ".png"}

// extFor is the file extension of a generated file's media type.
func extFor(mime string) string { return cmp.Or(fileExts[mime], ".bin") }

// write stores data as a new asset file of the Project and returns the asset (without size checks
// beyond len(data)). The file is written atomically, so a crash never leaves a partial file.
func (f fileStore) write(projectID string, typ domain.AssetType, mime string, data []byte) (domain.Asset, error) {
	return f.store(projectID, typ, mime, modeladapter.BlobOf(data))
}

// store makes content a new asset file of the Project and returns the asset. The content is
// consumed: a staged download is moved into place (or removed on failure), never read into
// memory. The file only appears at its final path complete.
func (f fileStore) store(projectID string, typ domain.AssetType, mime string, content modeladapter.Blob) (domain.Asset, error) {
	if f.root == "" {
		content.Discard()
		return domain.Asset{}, fmt.Errorf("assets directory is not configured")
	}
	id := domain.NewID()
	rel := filepath.Join(projectID, id+extFor(mime))
	abs := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), assetDirPerm); err != nil {
		content.Discard()
		return domain.Asset{}, fmt.Errorf("create asset directory: %w", err)
	}
	if err := content.StoreAs(abs, assetFilePerm); err != nil {
		return domain.Asset{}, fmt.Errorf("write asset file: %w", err)
	}
	return domain.Asset{
		ID: id, ProjectID: projectID, Type: typ, FilePath: filepath.ToSlash(rel), MimeType: mime, SizeBytes: content.Size(),
	}, nil
}

// remove deletes the file of an asset that was not stored in the database.
func (f fileStore) remove(a domain.Asset) {
	_ = os.Remove(filepath.Join(f.root, filepath.FromSlash(a.FilePath)))
}

// read returns the content of an image asset, or an error when it is missing or too large.
func (f fileStore) read(a domain.Asset) ([]byte, error) {
	abs := filepath.Join(f.root, filepath.FromSlash(a.FilePath))
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat asset %s: %w", a.ID, err)
	}
	if info.Size() > maxInputBytes {
		return nil, fmt.Errorf("asset %s is too large to use as a reference", a.ID)
	}
	data, err := os.ReadFile(abs) // #nosec G304 -- the path is built from the asset row under the assets directory
	if err != nil {
		return nil, fmt.Errorf("read asset %s: %w", a.ID, err)
	}
	return data, nil
}

// frameRate is the frame rate of the asset's video for its Take card; 0 when unknown (the player measures it).
func (f fileStore) frameRate(ctx context.Context, a domain.Asset) float64 {
	if f.media == nil {
		return 0
	}
	info, _ := f.media.Probe(ctx, filepath.Join(f.root, filepath.FromSlash(a.FilePath))) // no Info on error
	return info.FPS
}
