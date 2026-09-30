package generation

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

const (
	assetDirPerm  = 0o700
	assetFilePerm = 0o600
	// maxInputBytes bounds a stored image handed to a model as input.
	maxInputBytes = 20 << 20
)

// fileStore reads and writes the files of assets under the assets directory.
type fileStore struct{ root string }

// extFor is the file extension of a generated file's media type.
func extFor(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "video/mp4":
		return ".mp4"
	case "image/png":
		return ".png"
	}
	return ".bin"
}

// write stores data as a new asset file of the Project and returns the asset (without size checks
// beyond len(data)). The file is written atomically, so a crash never leaves a partial file.
func (f fileStore) write(projectID string, typ domain.AssetType, mime string, data []byte) (domain.Asset, error) {
	if f.root == "" {
		return domain.Asset{}, fmt.Errorf("assets directory is not configured")
	}
	id := domain.NewID()
	rel := filepath.Join(projectID, id+extFor(mime))
	abs := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), assetDirPerm); err != nil {
		return domain.Asset{}, fmt.Errorf("create asset directory: %w", err)
	}
	if err := fsutil.WriteFileAtomic(abs, data, assetFilePerm); err != nil {
		return domain.Asset{}, fmt.Errorf("write asset file: %w", err)
	}
	return domain.Asset{
		ID: id, ProjectID: projectID, Type: typ, FilePath: filepath.ToSlash(rel), MimeType: mime, SizeBytes: int64(len(data)),
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
