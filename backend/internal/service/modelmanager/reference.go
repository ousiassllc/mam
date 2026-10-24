package modelmanager

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// stageInput writes the picture of a trial to the inputs directory and returns its file name and type.
func (s *Service) stageInput(r Reference) (name, mime string, err error) {
	if len(r.Data) == 0 || len(r.Data) > maxReferenceSize {
		return "", "", invalid("参照画像は 1 バイト以上 %d MiB 以下にしてください", maxReferenceSize>>20)
	}
	mime = http.DetectContentType(r.Data)
	ext, ok := referenceTypes[mime]
	if !ok {
		return "", "", invalid("参照画像は PNG・JPEG・WebP にしてください")
	}
	name = domain.NewID() + ext
	dir := filepath.Join(s.gentestDir(), inputsDirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, name), r.Data, 0o600); err != nil {
		return "", "", err
	}
	return name, mime, nil
}

// referenceTypes are the picture types a trial takes, with the extension of the stored file.
var referenceTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}

// StageReference stores the picture of a trial ahead of StartGentest and returns the ID to pass as
// GentestInput.ReferenceID (the MCP upload `model_trial_reference`). The picture is checked as Reference is; one that
// no trial took within 24 hours is removed (at the next start of mam).
func (s *Service) StageReference(r Reference) (id string, err error) {
	id, _, err = s.stageInput(r)
	return id, err
}

var stagedPattern = regexp.MustCompile(`^[0-9A-Za-z-]{8,64}(\.png|\.jpg|\.webp)$`)

// stagedMime is the type of a picture StageReference stored (ErrNotFound for one that is not there: used by a trial
// already, or removed).
func (s *Service) stagedMime(id string) (string, error) {
	m := stagedPattern.FindStringSubmatch(id)
	if m == nil {
		return "", invalid("参照画像の ID が正しくありません")
	}
	if _, err := os.Stat(filepath.Join(s.gentestDir(), inputsDirName, id)); err != nil { //nolint:gosec // id matches stagedPattern: no separator
		return "", invalid("その参照画像はありません（使用済みか、24 時間を過ぎて消えました）。もう一度アップロードしてください")
	}
	for mime, ext := range referenceTypes {
		if ext == m[1] {
			return mime, nil
		}
	}
	return "", invalid("参照画像の ID が正しくありません")
}
