package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/previewcache"
)

// previewDir is where the previews of Takes (proxies, thumbnails) are cached under the assets directory; made on demand, deletable any time.
const previewDir = ".previews"

const previewRetryAfter = 10 * time.Minute // a failure costs minutes (media.ErrTimeout); every Range request / card would pay it

// MediaFile is a file to serve.
type MediaFile struct {
	Path     string
	MimeType string
}

// Video returns the file the preview plays for the Take: the file itself, or, when the picture is taller
// than 1080 lines (4K), a 1080p proxy made on first use so that playback does not drop frames
// (spec/requirements/non-functional.md §2). Without ffmpeg, or while the proxy cannot be made, the file is played.
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
	a, _ := s.assets.Get(ctx, d.ProjectID, d.AssetID) // a zero Asset when it is missing
	original := MediaFile{Path: src, MimeType: cmp.Or(a.MimeType, "video/mp4")}
	if s.tools == nil {
		return original, nil
	}
	proxied := d.Proxied()
	if d.Resolution == "" { // not reported by the model: look at the file
		tall, err := s.previews.Tall(takeID, func() (bool, error) {
			info, err := s.tools.Probe(ctx, src)
			return info.Height > media.MaxPreviewHeight, err
		})
		if err == nil {
			proxied = tall
		}
	}
	if !proxied {
		return original, nil
	}
	dst := s.previewPath(takeID + ".mp4")
	if dst == "" {
		return original, nil // the ID is not a safe file name: no proxy is made
	}
	err = s.previews.Make(ctx, dst, previewRetryAfter, func(ctx context.Context) error { return s.tools.Proxy(ctx, src, dst) })
	if err == nil {
		return MediaFile{Path: dst, MimeType: "video/mp4"}, nil
	}
	if ctx.Err() == nil && !errors.Is(err, previewcache.ErrFailedRecently) { // a recent failure was logged; a client that went away (a seek) is no failure
		s.logger.Error("make proxy failed; playing the original", "take_id", takeID, "err", err)
	}
	return original, nil
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
	err = s.previews.Make(ctx, dst, previewRetryAfter, func(ctx context.Context) error { return s.tools.Thumbnail(ctx, src, dst, 0) })
	if errors.Is(err, previewcache.ErrFailedRecently) {
		return MediaFile{}, ErrNotFound // no thumbnail for now; the failure was logged when it happened
	}
	if err != nil {
		return MediaFile{}, fmt.Errorf("make thumbnail: %w", err)
	}
	return MediaFile{Path: dst, MimeType: "image/jpeg"}, nil
}
