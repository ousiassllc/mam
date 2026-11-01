package voicerefsvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// store checks the recording (extension, signature, size, length) and writes it below the directory of the
// owner. Nothing stays on disk when it is refused.
func (s *Service) store(ctx context.Context, ownerID string, f File) (voiceref.Reference, error) {
	if s.assetsDir == "" {
		return voiceref.Reference{}, errors.New("uploads are not configured: assets directory is empty")
	}
	ext := strings.ToLower(filepath.Ext(filepath.Base(f.Name)))
	format, ok := voiceref.FormatOf(ext)
	if !ok {
		return voiceref.Reference{}, invalid("file", "参照音声は %s のファイルを選んでください", strings.Join(voiceref.Extensions(), " "))
	}
	if f.Size <= 0 {
		return voiceref.Reference{}, invalid("file", "ファイルが空です")
	}
	if f.Size > voiceref.MaxBytes {
		return voiceref.Reference{}, invalid("file", "サイズが上限（%d MB）を超えています", voiceref.MaxBytes>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return voiceref.Reference{}, invalid("file", "ファイルを読み取れませんでした")
	}
	defer func() { _ = rc.Close() }()
	head := make([]byte, 512)
	n, err := io.ReadFull(rc, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return voiceref.Reference{}, invalid("file", "ファイルを読み取れませんでした")
	}
	head = head[:n]
	if !format.Matches(head) {
		return voiceref.Reference{}, invalid("file", "拡張子とファイルの内容が一致しません")
	}
	rel := ownerID + "/" + domain.NewID() + ext
	abs := filepath.Join(s.assetsDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return voiceref.Reference{}, fmt.Errorf("create the directory of the reference voice: %w", err)
	}
	var written int64
	err = fsutil.WriteAtomic(abs, 0o600, func(w io.Writer) (copyErr error) {
		written, copyErr = io.Copy(w, io.LimitReader(io.MultiReader(bytes.NewReader(head), rc), voiceref.MaxBytes+1))
		return copyErr
	})
	if err != nil {
		return voiceref.Reference{}, fmt.Errorf("store the reference voice: %w", err)
	}
	ref := voiceref.Reference{FilePath: rel, MimeType: format.MimeType, SizeBytes: written}
	if err := s.check(ctx, abs, &ref); err != nil {
		_ = os.Remove(abs)
		return voiceref.Reference{}, err
	}
	return ref, nil
}

// check refuses a recording over the size limit and, with ffprobe, one that is not audio or whose length is
// outside voiceref.MinDuration..MaxDuration (GPT-SoVITS rejects it). Without ffprobe the length is not known:
// the recording is accepted with DurationMS 0, and the voice provider checks it when it runs.
func (s *Service) check(ctx context.Context, abs string, ref *voiceref.Reference) error {
	if ref.SizeBytes > voiceref.MaxBytes {
		return invalid("file", "サイズが上限（%d MB）を超えています", voiceref.MaxBytes>>20)
	}
	if s.probe == nil {
		return nil
	}
	seconds, err := s.probe.AudioDuration(ctx, abs)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return invalid("file", "音声として読み取れませんでした（壊れているか、対応していない中身です）")
	}
	ms := int(seconds*1000 + 0.5)
	if d := time.Duration(ms) * time.Millisecond; d < voiceref.MinDuration || d > voiceref.MaxDuration {
		return invalid("file", "参照音声は %d〜%d 秒にしてください（この音声は %.1f 秒です）",
			int(voiceref.MinDuration/time.Second), int(voiceref.MaxDuration/time.Second), seconds)
	}
	ref.DurationMS = ms
	return nil
}

// discard removes the file of old once no Character refers to it (it is shared by the copies): it is no
// longer the voice of any row. keep is the reference that was just set; its file stays. A failure is logged:
// the voice is set either way.
func (s *Service) discard(ctx context.Context, old, keep *voiceref.Reference) {
	if old == nil || (keep != nil && old.FilePath == keep.FilePath) {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if used, err := s.repo.InUse(ctx, old.FilePath); err != nil || used {
		if err != nil {
			s.logger.Error("check the reference voice file failed", "path", old.FilePath, "err", err)
		}
		return
	}
	if abs := s.path(old.FilePath); abs != "" {
		if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logger.Error("remove the reference voice file failed", "path", old.FilePath, "err", err)
		}
	}
}

// path is the absolute path of a stored file, "" for one that is not below the assets directory.
func (s *Service) path(rel string) string {
	if rel == "" || s.assetsDir == "" {
		return ""
	}
	abs := filepath.Join(s.assetsDir, filepath.FromSlash(rel))
	within, err := filepath.Rel(s.assetsDir, abs)
	if err != nil || within == "." || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return ""
	}
	return abs
}
