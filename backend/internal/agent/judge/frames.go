package judge

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/ousiassllc/mam/backend/internal/media"
)

// ErrNoFFmpeg means ffmpeg / ffprobe are not installed, so no frame can be taken from a Take.
var ErrNoFFmpeg = fmt.Errorf("judge: ffmpeg and ffprobe are required to sample frames: %w", media.ErrUnavailable)

// Frame is one sampled frame of a Take.
type Frame struct {
	// Time is the position in seconds; Number is the source frame number at that time.
	Time   float64
	Number int
	JPEG   []byte
}

// Clip is the frames sampled from a Take.
type Clip struct {
	// Duration is the length in seconds and FPS the frame rate of the video.
	Duration float64
	FPS      float64
	// Interval is the time between two sampled frames.
	Interval float64
	Frames   []Frame
}

// FrameRange converts a time range in seconds to the first and last source frame number, both
// inclusive of the frame at their time (2.1-2.8 s at 30 fps is frames 63-84, as in spec §12.3).
func (c Clip) FrameRange(from, to float64) (first, last int) {
	total := max(int(math.Round(c.Duration*c.FPS))-1, 0)
	first = min(max(int(math.Round(from*c.FPS)), 0), total)
	last = min(max(int(math.Round(to*c.FPS)), first), total)
	return first, last
}

// SampleSpec says which frames to take: FPS per second, at most MaxFrames (a longer video is
// sampled more sparsely), scaled down to MaxWidth pixels when wider.
type SampleSpec struct {
	FPS       float64
	MaxFrames int
	MaxWidth  int
}

// FrameSource takes frames from a video file.
type FrameSource interface {
	Sample(ctx context.Context, videoPath string, spec SampleSpec) (Clip, error)
}

// FFmpeg is the FrameSource that runs ffmpeg; the video is probed through media.Tools.
type FFmpeg struct {
	tools media.Tools
}

// LookFFmpeg finds ffmpeg and ffprobe on the PATH. It returns ErrNoFFmpeg when either is missing.
func LookFFmpeg() (FFmpeg, error) {
	tools, err := media.Look()
	if err != nil {
		return FFmpeg{}, ErrNoFFmpeg
	}
	return FFmpeg{tools: tools}, nil
}

// Sample implements FrameSource.
func (f FFmpeg) Sample(ctx context.Context, videoPath string, spec SampleSpec) (Clip, error) {
	info, err := f.tools.Probe(ctx, videoPath)
	if err != nil {
		return Clip{}, err
	}
	duration, fps := info.Duration, info.FPS
	rate := min(spec.FPS, float64(spec.MaxFrames)/duration)
	clip := Clip{Duration: duration, FPS: fps, Interval: 1 / rate}

	dir, err := os.MkdirTemp("", "mam-frames-")
	if err != nil {
		return Clip{}, fmt.Errorf("frame directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	filter := fmt.Sprintf("fps=%.6f,scale='min(%d,iw)':-2", rate, spec.MaxWidth)
	if err := f.tools.Frames(ctx, videoPath, filepath.Join(dir, "%04d.jpg"), filter, spec.MaxFrames, duration); err != nil {
		return Clip{}, err
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if err != nil {
		return Clip{}, err
	}
	sort.Strings(names)
	for i, name := range names {
		data, err := os.ReadFile(name) // #nosec G304 -- a file ffmpeg just wrote into our temporary directory
		if err != nil {
			return Clip{}, fmt.Errorf("read frame: %w", err)
		}
		t := float64(i) * clip.Interval
		number, _ := clip.FrameRange(t, t)
		clip.Frames = append(clip.Frames, Frame{Time: t, Number: number, JPEG: data})
	}
	if len(clip.Frames) == 0 {
		return Clip{}, fmt.Errorf("ffmpeg: no frame could be read from %s", filepath.Base(videoPath))
	}
	return clip, nil
}

// evenly returns n frames spread evenly over the clip's frames (all of them when n covers them).
func evenly(frames []Frame, n int) []Frame {
	if n >= len(frames) {
		return frames
	}
	out := make([]Frame, 0, n)
	for i := range n {
		out = append(out, frames[i*len(frames)/n])
	}
	return out
}
