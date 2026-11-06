// Package finalframe takes the last frame of the Take that the next Shot continues from (Continue, F-16): the Take to read is the
// selected one, else the AI pick, else the newest one that was not rejected. It never fails Continue: without a frame the Agent works
// from the text.
package finalframe

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
)

// Source is where the frames come from: the ffmpeg of Tools (the one the app holds now), the files under AssetPath (it returns "" for
// a path outside the assets directory, which is never read) and the log of what went wrong.
type Source struct {
	Tools     *media.Tools
	AssetPath func(rel string) string
	Logger    *slog.Logger
}

func rank(t domain.Take) int {
	switch t.Status {
	case domain.TakeSelected:
		return 3
	case domain.TakeAIPick:
		return 2
	case domain.TakeRejected:
		return 0
	}
	return 1
}

// Of returns the last frame of the best Take of takes as a JPEG, or nil when there is none: no ffmpeg, no video file, or ffmpeg failed.
func (s Source) Of(ctx context.Context, takes []domain.Take) *llm.Image {
	if !s.Tools.Available() {
		return nil
	}
	var from *domain.Take
	for i, t := range takes {
		if s.AssetPath(t.FilePath) == "" || rank(t) == 0 { // no file, or one outside the assets directory (never read)
			continue
		}
		if from == nil || rank(t) >= rank(*from) {
			from = &takes[i]
		}
	}
	if from == nil {
		return nil
	}
	src := s.AssetPath(from.FilePath)
	info, err := s.Tools.Probe(ctx, src)
	if err != nil {
		s.Logger.Error("probe the take for the final frame failed", "take_id", from.ID, "err", err)
		return nil
	}
	dir, err := os.MkdirTemp("", "mam-final-frame-")
	if err != nil {
		s.Logger.Error("create a directory for the final frame failed", "err", err)
		return nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	dst := filepath.Join(dir, "final.jpg")
	if err := s.Tools.Thumbnail(ctx, src, dst, info.Duration-0.1); err != nil {
		s.Logger.Error("take the final frame failed", "take_id", from.ID, "err", err)
		return nil
	}
	data, err := os.ReadFile(dst) // #nosec G304 -- a file this function just wrote
	if err != nil {
		s.Logger.Error("read the final frame failed", "take_id", from.ID, "err", err)
		return nil
	}
	return &llm.Image{MimeType: "image/jpeg", Data: data}
}
