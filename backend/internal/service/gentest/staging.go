package gentest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

const (
	stagingDirName = "_staging"
	stagingMaxAge  = 24 * time.Hour
)

// inputTypes are the image types a batch takes, with the extension of the stored file.
var inputTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}

var stagedPattern = regexp.MustCompile(`^[0-9A-Za-z-]{8,64}(\.png|\.jpg|\.webp)$`)

// StageReference stores an uploaded image until a batch takes it and returns the staged id (RefInput.ID with Source
// FromStaged) and the type. The image must be a PNG, JPEG or WebP of 1 byte to 20 MiB. One that no batch took within 24 hours
// is removed (at the next start of mam).
func (s *Service) StageReference(data []byte) (id, mime string, err error) {
	if len(data) == 0 || len(data) > MaxImageBytes {
		return "", "", gterr.Invalid("画像は 1 バイト以上 %d MiB 以下にしてください", MaxImageBytes>>20)
	}
	mime = http.DetectContentType(data)
	ext, ok := inputTypes[mime]
	if !ok {
		return "", "", gterr.Invalid("画像は PNG・JPEG・WebP にしてください")
	}
	dir := filepath.Join(s.dir(), stagingDirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", err
	}
	id = domain.NewID() + ext
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, id), data, 0o600); err != nil {
		return "", "", err
	}
	return id, mime, nil
}

// StagedFile is the path and the type of a staged image (gterr.ErrNotFound for one that is not there: used by a batch already, or
// removed after 24 hours).
func (s *Service) StagedFile(id string) (path, mime string, err error) {
	m := stagedPattern.FindStringSubmatch(id)
	if m == nil {
		return "", "", gterr.ErrNotFound
	}
	path = filepath.Join(s.dir(), stagingDirName, id) //nolint:gosec // id matches stagedPattern: no separator
	if _, err := os.Stat(path); err != nil {
		return "", "", gterr.ErrNotFound
	}
	for mime, ext := range inputTypes {
		if ext == m[1] {
			return path, mime, nil
		}
	}
	return "", "", gterr.ErrNotFound
}

func (s *Service) cleanStaging() {
	dir := filepath.Join(s.dir(), stagingDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && s.Now().Sub(info.ModTime()) > stagingMaxAge {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// source is an input image found: the file, its type and where it came from.
type source struct {
	path   string
	mime   string
	size   int64
	origin json.RawMessage
}

// resolve finds the file of an input image. A source that is not there (a deleted Project asset, a staged image that was taken
// or expired, a history result that is not an image) is *gterr.InvalidError: the request names something that cannot be used.
func (s *Service) resolve(ctx context.Context, ref RefInput) (source, error) {
	var path, mime string
	switch ref.Source {
	case FromStaged:
		p, m, err := s.StagedFile(ref.ID)
		if err != nil {
			return source{}, gterr.Invalid("アップロードした画像 %q はありません（使用済みか、24 時間を過ぎて消えました）。もう一度アップロードしてください", ref.ID)
		}
		path, mime = p, m
	case FromProjectAsset, FromReferenceItem:
		p, m, err := s.projectFile(ctx, ref)
		if err != nil {
			return source{}, err
		}
		path, mime = p, m
	case FromHistory:
		res, err := s.Repo.GetResult(ctx, ref.ID)
		if errors.Is(err, repository.ErrNotFound) || (err == nil && (res.Media != gentestrepo.MediaImage || res.Status != gentestrepo.StatusSucceeded || res.FilePath == "")) {
			return source{}, gterr.Invalid("履歴の結果 %q は画像として使えません（成功した画像だけ使えます）", ref.ID)
		}
		if err != nil {
			return source{}, err
		}
		path, mime = filepath.Join(s.dir(), res.FilePath), res.MimeType
	}
	info, err := os.Stat(path)
	if err != nil {
		return source{}, gterr.Invalid("入力画像 %q のファイルが見つかりません", ref.ID)
	}
	if info.Size() == 0 || info.Size() > MaxImageBytes {
		return source{}, gterr.Invalid("入力画像 %q は 1 バイト以上 %d MiB 以下にしてください", ref.ID, MaxImageBytes>>20)
	}
	if _, ok := inputTypes[mime]; !ok {
		return source{}, gterr.Invalid("入力画像 %q は PNG・JPEG・WebP にしてください（今は %s）", ref.ID, mime)
	}
	origin, _ := json.Marshal(map[string]string{"kind": ref.Source, "id": ref.ID, "project_id": ref.ProjectID})
	return source{path: path, mime: mime, size: info.Size(), origin: origin}, nil
}

// projectFile finds the image of a Project's asset or Reference Library entry inside the assets directory.
func (s *Service) projectFile(ctx context.Context, ref RefInput) (path, mime string, err error) {
	assetID := ref.ID
	if ref.Source == FromReferenceItem {
		item, err := s.References.Get(ctx, ref.ProjectID, ref.ID)
		if errors.Is(err, repository.ErrNotFound) {
			return "", "", gterr.Invalid("プロジェクトの参照画像 %q が見つかりません", ref.ID)
		}
		if err != nil {
			return "", "", err
		}
		assetID = item.AssetID
	}
	a, err := s.Assets.Get(ctx, ref.ProjectID, assetID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", "", gterr.Invalid("プロジェクトの画像 %q が見つかりません", assetID)
	}
	if err != nil {
		return "", "", err
	}
	if a.Type == domain.AssetVideo || !strings.HasPrefix(a.MimeType, "image/") {
		return "", "", gterr.Invalid("プロジェクトの %q は画像ではありません", assetID)
	}
	path = filepath.Join(s.AssetsDir, filepath.FromSlash(a.FilePath))
	if rel, err := filepath.Rel(s.AssetsDir, path); err != nil || strings.HasPrefix(rel, "..") {
		return "", "", gterr.Invalid("プロジェクトの画像 %q の場所が正しくありません", assetID)
	}
	return path, a.MimeType, nil
}
