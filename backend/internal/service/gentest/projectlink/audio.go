package projectlink

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// sniffBytes is how much of an audio asset is read to check its kind again (the upload checked the same bytes; the file is not decoded here).
const sniffBytes = 512

// ProjectAudio is an audio file of a Project that a batch can take as the audio input of a speech-to-video model: an asset of type audio (Issue #1153).
type ProjectAudio struct {
	AssetID   string    `json:"asset_id"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// ProjectAudios lists the audio assets of a Project, newest first (gterr.ErrNotFound for an unknown Project).
func (l *Link) ProjectAudios(ctx context.Context, projectID string) ([]ProjectAudio, error) {
	if _, err := l.Projects.Get(ctx, projectID); errors.Is(err, repository.ErrNotFound) {
		return nil, gterr.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	list, err := l.Repo.ProjectAudios(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectAudio, len(list))
	for i, a := range list {
		out[i] = ProjectAudio{AssetID: a.AssetID, MimeType: a.MimeType, SizeBytes: a.SizeBytes, CreatedAt: a.CreatedAt}
	}
	return out, nil
}

// AssetInputFile finds the file of the input a batch names in a Project: the audio file of an asset when audio is true (a Reference Library entry is never audio),
// else the image of an asset or of the Reference Library entry (reference).
func (l *Link) AssetInputFile(ctx context.Context, projectID, id string, reference, audio bool) (path, mime string, err error) {
	if audio {
		if reference {
			return "", "", gterr.Invalid("音声ファイルは参照画像から選べません")
		}
		return l.AudioFile(ctx, projectID, id)
	}
	return l.ImageFile(ctx, projectID, id, reference)
}

// AudioFile finds the audio file of a Project's asset inside the assets directory. The asset must be of the Project and of type audio (a picture, a video or a
// Reference Library image is refused, and so is a file whose bytes are not the audio kind the asset says); the media type is the one audiofile names.
func (l *Link) AudioFile(ctx context.Context, projectID, assetID string) (path, mime string, err error) {
	a, err := l.Assets.Get(ctx, projectID, assetID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", "", gterr.Invalid("プロジェクトの音声ファイル %q が見つかりません", assetID)
	}
	if err != nil {
		return "", "", err
	}
	if a.Type != domain.AssetAudio || !audiofile.Is(a.MimeType) {
		return "", "", gterr.Invalid("プロジェクトの %q は音声ファイルではありません（音声の入力に使えるのは、アセットの種別が音声のものだけです）", assetID)
	}
	path = filepath.Join(l.AssetsDir, filepath.FromSlash(a.FilePath))
	if rel, relErr := filepath.Rel(l.AssetsDir, path); relErr != nil || strings.HasPrefix(rel, "..") {
		return "", "", gterr.Invalid("プロジェクトの音声ファイル %q の場所が正しくありません", assetID)
	}
	head, err := readHead(path)
	if err != nil {
		return "", "", gterr.Invalid("プロジェクトの音声ファイル %q のファイルが見つかりません", assetID)
	}
	if kind, ok := audiofile.Sniff(head); !ok || kind != a.MimeType {
		return "", "", gterr.Invalid("プロジェクトの音声ファイル %q の中身が %s ではありません", assetID, a.MimeType)
	}
	return path, a.MimeType, nil
}

func readHead(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // path is inside the assets directory (checked by the caller)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	head, err := io.ReadAll(io.LimitReader(f, sniffBytes))
	return head, err
}
