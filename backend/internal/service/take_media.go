package service

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/media"
)

// previewDir is where the previews of Takes (proxies and thumbnails) are cached, under the assets
// directory. They are made on demand and can be deleted at any time.
const previewDir = ".previews"

// MediaFile is a file to serve.
type MediaFile struct {
	Path     string
	MimeType string
}

// Video returns the file the preview plays for the Take: the file itself, or, when the picture is
// taller than 1080 lines (4K), a 1080p proxy made on first use so that playback does not drop
// frames (spec/requirements/non-functional.md §2). Without ffmpeg the file itself is played.
func (s *TakeService) Video(ctx context.Context, takeID string) (MediaFile, error) {
	d, err := s.takes.Get(ctx, takeID)
	if err != nil {
		return MediaFile{}, notFound(err)
	}
	if d.FilePath == "" {
		return MediaFile{}, ErrNotFound // archived
	}
	src := s.assetPath(d.FilePath)
	if src == "" {
		return MediaFile{}, ErrNotFound // outside the assets directory
	}
	mime := "video/mp4"
	if a, err := s.assets.Get(ctx, d.ProjectID, d.AssetID); err == nil && a.MimeType != "" {
		mime = a.MimeType
	}
	if s.tools == nil {
		return MediaFile{Path: src, MimeType: mime}, nil
	}
	proxied := d.Proxied()
	if d.Resolution == "" { // not reported by the model: look at the file
		if info, err := s.tools.Probe(ctx, src); err == nil {
			proxied = info.Height > media.MaxPreviewHeight
		}
	}
	if !proxied {
		return MediaFile{Path: src, MimeType: mime}, nil
	}
	dst := s.previewPath(takeID + ".mp4")
	if dst == "" {
		return MediaFile{Path: src, MimeType: mime}, nil // the ID is not a safe file name: no proxy is made
	}
	if err := s.makePreview(dst, func() error { return s.tools.Proxy(ctx, src, dst) }); err != nil {
		s.logger.Error("make proxy failed; playing the original", "take_id", takeID, "err", err)
		return MediaFile{Path: src, MimeType: mime}, nil
	}
	return MediaFile{Path: dst, MimeType: "video/mp4"}, nil
}

// Thumbnail returns a JPEG of the first frame of the Take (ErrUnavailable without ffmpeg).
func (s *TakeService) Thumbnail(ctx context.Context, takeID string) (MediaFile, error) {
	d, err := s.takes.Get(ctx, takeID)
	if err != nil {
		return MediaFile{}, notFound(err)
	}
	if d.FilePath == "" {
		return MediaFile{}, ErrNotFound
	}
	if s.tools == nil {
		return MediaFile{}, ErrUnavailable
	}
	src := s.assetPath(d.FilePath)
	if src == "" {
		return MediaFile{}, ErrNotFound // outside the assets directory
	}
	dst := s.previewPath(takeID + ".jpg")
	if dst == "" {
		return MediaFile{}, ErrNotFound // the ID is not a safe file name
	}
	if err := s.makePreview(dst, func() error { return s.tools.Thumbnail(ctx, src, dst, 0) }); err != nil {
		return MediaFile{}, fmt.Errorf("make thumbnail: %w", err)
	}
	return MediaFile{Path: dst, MimeType: "image/jpeg"}, nil
}

// makePreview runs make unless the preview file already exists; concurrent calls for one file wait
// for the first.
func (s *TakeService) makePreview(dst string, make func() error) error {
	unlock := lockFor(&s.preview, dst)
	defer unlock()
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	return make()
}

// lockFor locks the mutex of key in m (created on first use) and returns its unlock.
func lockFor(m *sync.Map, key string) func() {
	mu, _ := m.LoadOrStore(key, &sync.Mutex{})
	l := mu.(*sync.Mutex)
	l.Lock()
	return l.Unlock
}
