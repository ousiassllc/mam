package gentest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"

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

// InputExt is the extension of the stored file of an image type a batch takes ("" for any other).
func InputExt(mime string) string { return inputTypes[mime] }

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

// Source is an input image found: the file, its type and where it came from.
type Source struct {
	Path   string
	Mime   string
	Size   int64
	Origin json.RawMessage
}

// ResolveInput finds the file of an input image (the prompt match copies it into its batch). A source that is not there (a deleted Project asset, a staged image that was taken
// or expired, a history result that is not an image) is *gterr.InvalidError: the request names something that cannot be used.
func (s *Service) ResolveInput(ctx context.Context, ref RefInput) (Source, error) {
	var path, mime string
	switch ref.Source {
	case FromStaged:
		p, m, err := s.StagedFile(ref.ID)
		if err != nil {
			return Source{}, gterr.Invalid("アップロードした画像 %q はありません（使用済みか、24 時間を過ぎて消えました）。もう一度アップロードしてください", ref.ID)
		}
		path, mime = p, m
	case FromProjectAsset, FromReferenceItem:
		p, m, err := s.ImageFile(ctx, ref.ProjectID, ref.ID, ref.Source == FromReferenceItem)
		if err != nil {
			return Source{}, err
		}
		path, mime = p, m
	case FromPoseLibrary:
		p, err := s.poseFile(ref.ID)
		if err != nil {
			return Source{}, err
		}
		path, mime = p, "image/webp"
	case FromHistory:
		res, err := s.Repo.GetResult(ctx, ref.ID)
		if errors.Is(err, repository.ErrNotFound) || (err == nil && (res.Media != gentestrepo.MediaImage || res.Status != gentestrepo.StatusSucceeded || res.FilePath == "")) {
			return Source{}, gterr.Invalid("履歴の結果 %q は画像として使えません（成功した画像だけ使えます）", ref.ID)
		}
		if err != nil {
			return Source{}, err
		}
		if _, ok := inputTypes[res.MimeType]; !ok { // a GIF or an SVG of GPT Image 2.6: no model of mam takes it as an input, and the face and upscale tools read pixels
			return Source{}, gterr.Invalid("履歴の結果 %q は %s のため入力画像にできません（入力にできるのは PNG・JPEG・WebP の結果だけ）", ref.ID, mimeLabel(res.MimeType))
		}
		path, mime = filepath.Join(s.dir(), res.FilePath), res.MimeType
	case FromBatchInput:
		in, err := s.Repo.GetInput(ctx, ref.ID)
		if errors.Is(err, repository.ErrNotFound) || (err == nil && s.InputHidden(ctx, in)) { // a copy of an adult pose picture is as good as gone while the age gate is closed
			return Source{}, gterr.Invalid("過去の入力画像 %q は残っていません（その結果をすべて削除すると、入力画像も消えます）", ref.ID)
		}
		if err != nil {
			return Source{}, err
		}
		path, mime = filepath.Join(s.dir(), filepath.FromSlash(in.FilePath)), in.MimeType
	}
	info, err := os.Stat(path)
	if err != nil {
		return Source{}, gterr.Invalid("入力画像 %q のファイルが見つかりません", ref.ID)
	}
	if info.Size() == 0 || info.Size() > MaxImageBytes {
		return Source{}, gterr.Invalid("入力画像 %q は 1 バイト以上 %d MiB 以下にしてください", ref.ID, MaxImageBytes>>20)
	}
	if _, ok := inputTypes[mime]; !ok {
		return Source{}, gterr.Invalid("入力画像 %q は PNG・JPEG・WebP にしてください（今は %s）", ref.ID, mime)
	}
	origin, _ := json.Marshal(map[string]string{"kind": ref.Source, "id": ref.ID, "project_id": ref.ProjectID})
	return Source{Path: path, Mime: mime, Size: info.Size(), Origin: origin}, nil
}

// mimeLabel names the type of a result file for a refusal: GIF and SVG are the ones a model can make besides the three input types.
func mimeLabel(mime string) string {
	switch mime {
	case "image/gif":
		return "GIF"
	case "image/svg+xml":
		return "SVG"
	}
	return mime
}

// poseFile finds the picture of the pose library (a refusal that says what to do when the library is not installed).
func (s *Service) poseFile(id string) (string, error) {
	if s.Poses == nil {
		return "", gterr.Invalid("ポーズ集は使えません")
	}
	path, err := s.Poses.File(id)
	if errors.Is(err, poses.ErrNotInstalled) {
		return "", gterr.Invalid("ポーズ集が導入されていません。単発生成の「ポーズを選ぶ」から導入してください")
	}
	return path, err
}
