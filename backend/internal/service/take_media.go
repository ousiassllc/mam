package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/previewcache"
)

// previewDir is where the previews of Takes (proxies and thumbnails) are cached, under the assets
// directory. They are made on demand and can be deleted at any time.
const previewDir = ".previews"

// previewRetryAfter is how long a preview that failed to be made (a proxy, a thumbnail) is not made
// again. A failure costs minutes (media.ErrTimeout) and the browser asks again on every Range
// request and every card.
const previewRetryAfter = 10 * time.Minute

// MediaFile is a file to serve.
type MediaFile struct {
	Path     string
	MimeType string
}

// Video returns the file the preview plays for the Take: the file itself, or, when the picture is
// taller than 1080 lines (4K), a 1080p proxy made on first use so that playback does not drop
// frames (spec/requirements/non-functional.md §2). Without ffmpeg, or while the proxy cannot be
// made, the file itself is played.
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
	original := MediaFile{Path: src, MimeType: mime}
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
	if ctx.Err() != nil {
		return MediaFile{}, ctx.Err() // the client went away (a seek, a page change): not a failure, and nobody to play the original to
	}
	if !errors.Is(err, previewcache.ErrFailedRecently) { // a recent failure was logged when it happened
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
	switch {
	case errors.Is(err, previewcache.ErrFailedRecently):
		return MediaFile{}, ErrNotFound // no thumbnail for this Take for now; the failure was reported when it happened
	case err != nil:
		return MediaFile{}, fmt.Errorf("make thumbnail: %w", err)
	}
	return MediaFile{Path: dst, MimeType: "image/jpeg"}, nil
}
