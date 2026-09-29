package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrNoFFmpeg means ffmpeg / ffprobe are not installed, so no frame can be taken from a Take.
var ErrNoFFmpeg = errors.New("judge: ffmpeg and ffprobe are required to sample frames")

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

// FFmpeg is the FrameSource that runs ffmpeg and ffprobe.
type FFmpeg struct{ ffmpeg, ffprobe string }

// LookFFmpeg finds ffmpeg and ffprobe on the PATH. It returns ErrNoFFmpeg when either is missing.
func LookFFmpeg() (FFmpeg, error) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		return FFmpeg{}, ErrNoFFmpeg
	}
	return FFmpeg{ffmpeg: ffmpeg, ffprobe: ffprobe}, nil
}

type probeOutput struct {
	Streams []struct {
		RFrameRate string `json:"r_frame_rate"`
		Duration   string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// probe returns the duration (seconds) and frame rate of the first video stream.
func (f FFmpeg) probe(ctx context.Context, path string) (duration, fps float64, err error) {
	cmd := exec.CommandContext(ctx, f.ffprobe, "-v", "error", "-select_streams", "v:0", // #nosec G204 -- fixed arguments; path is an asset file
		"-show_entries", "stream=r_frame_rate,duration:format=duration", "-of", "json", path)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe: %w", err)
	}
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil || len(p.Streams) == 0 {
		return 0, 0, fmt.Errorf("ffprobe: %s has no video stream", filepath.Base(path))
	}
	duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	if duration <= 0 {
		duration, _ = strconv.ParseFloat(p.Streams[0].Duration, 64)
	}
	if duration <= 0 {
		return 0, 0, fmt.Errorf("ffprobe: %s has no duration", filepath.Base(path))
	}
	fps = parseRate(p.Streams[0].RFrameRate)
	if fps <= 0 {
		fps = 24
	}
	return duration, fps, nil
}

// parseRate reads "30000/1001" or "24".
func parseRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	if !ok {
		return n
	}
	d, err := strconv.ParseFloat(den, 64)
	if err != nil || d == 0 {
		return 0
	}
	return n / d
}

// Sample implements FrameSource.
func (f FFmpeg) Sample(ctx context.Context, videoPath string, spec SampleSpec) (Clip, error) {
	duration, fps, err := f.probe(ctx, videoPath)
	if err != nil {
		return Clip{}, err
	}
	rate := min(spec.FPS, float64(spec.MaxFrames)/duration)
	clip := Clip{Duration: duration, FPS: fps, Interval: 1 / rate}

	dir, err := os.MkdirTemp("", "mam-frames-")
	if err != nil {
		return Clip{}, fmt.Errorf("frame directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	filter := fmt.Sprintf("fps=%.6f,scale='min(%d,iw)':-2", rate, spec.MaxWidth)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, f.ffmpeg, "-v", "error", "-i", videoPath, "-an", "-vf", filter, // #nosec G204 -- fixed arguments; path is an asset file
		"-frames:v", strconv.Itoa(spec.MaxFrames), "-q:v", "3", filepath.Join(dir, "%04d.jpg"))
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Clip{}, ctx.Err()
		}
		return Clip{}, fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
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
