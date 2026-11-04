package venhance

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/media"
)

// Prober reads the facts of a video file (ffprobe: media.Tools). A nil Prober is a PC without ffmpeg: nothing can be enhanced then, because the
// facts of the source are read from the file, never taken from what was asked of the generation.
type Prober interface {
	Probe(ctx context.Context, path string) (media.Info, error)
}

// SourceOf is the Source of a probed file: container is the lower case extension of its name without the dot, size the size of the file.
func SourceOf(info media.Info, container string, size int64) cloudenhance.Source {
	return cloudenhance.Source{
		Container: container, SizeBytes: size, Duration: info.Duration, FrameCount: info.FrameCount, FPS: info.FPS,
		Width: info.Width, Height: info.Height, HasAudio: info.HasAudio,
	}
}
