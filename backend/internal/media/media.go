// Package media runs ffmpeg and ffprobe on the video files of Takes: it probes them, makes the
// poster frame and the 1080p proxy of a 4K Take (spec/requirements/non-functional.md §2) and cuts
// Takes together with a cross-fade at every joint (Partial Take Use, spec/spec.md §14).
package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrUnavailable means ffmpeg / ffprobe are not installed.
var ErrUnavailable = errors.New("media: ffmpeg and ffprobe are required")

// MaxPreviewHeight is the tallest picture the preview plays as it is; a taller Take (4K) is
// shown through its proxy.
const MaxPreviewHeight = 1080

// Tools is the ffmpeg and ffprobe executables.
type Tools struct {
	ffmpeg, ffprobe string
	lim             limits // the zero value means defaultLimits
}

// Look finds ffmpeg and ffprobe on the PATH. It returns ErrUnavailable when either is missing.
func Look() (Tools, error) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		return Tools{}, ErrUnavailable
	}
	return Tools{ffmpeg: ffmpeg, ffprobe: ffprobe, lim: defaultLimits}, nil
}

// Info describes the first video stream of a file.
type Info struct {
	// Duration is the length in seconds.
	Duration float64
	// FPS is the frame rate; FrameRate is the same as ffprobe writes it ("30000/1001").
	FPS       float64
	FrameRate string
	Width     int
	Height    int
	HasAudio  bool
	// VideoDuration is the length of the video stream alone, 0 when the container does not tell:
	// shorter than Duration when an audio track is longer than the picture.
	VideoDuration float64
}

type probeOutput struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		RFrameRate string `json:"r_frame_rate"`
		Duration   string `json:"duration"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe reads the length, frame rate, size and audio of the video file.
func (t Tools) Probe(ctx context.Context, path string) (Info, error) {
	out, _, err := run(ctx, t.limits().probe, t.ffprobe, "-v", "error",
		"-show_entries", "stream=codec_type,r_frame_rate,duration,width,height:format=duration", "-of", "json", path)
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	var info Info
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if info.Width != 0 {
				continue // the first video stream only
			}
			info.Width, info.Height = s.Width, s.Height
			info.FrameRate = s.RFrameRate
			info.FPS = parseRate(s.RFrameRate)
			info.VideoDuration, _ = strconv.ParseFloat(s.Duration, 64)
		case "audio":
			info.HasAudio = true
		}
	}
	if info.Width == 0 {
		return Info{}, fmt.Errorf("ffprobe: %s has no video stream", filepath.Base(path))
	}
	if info.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64); info.Duration <= 0 {
		info.Duration = info.VideoDuration
	}
	if info.Duration <= 0 {
		return Info{}, fmt.Errorf("ffprobe: %s has no duration", filepath.Base(path))
	}
	if info.FPS <= 0 {
		info.FPS, info.FrameRate = 24, "24"
	}
	return info, nil
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

// Thumbnail writes the frame of src at the given second (clamped into the video) as a JPEG at most
// 960 pixels wide. A second past the last video frame (an audio track longer than the picture makes
// the container longer than the video) is no error of ffmpeg, which exits 0 and writes nothing: the
// last frame of the picture is taken then, and at last the first frame, so a frame always comes out
// of a video.
func (t Tools) Thumbnail(ctx context.Context, src, dst string, at float64) error {
	at = max(at, 0)
	err := t.thumbnailAt(ctx, src, dst, "-ss", strconv.FormatFloat(at, 'f', 3, 64))
	if !errors.Is(err, errNoOutput) {
		return err
	}
	if at <= 0 {
		return fmt.Errorf("ffmpeg: no frame in %s: %w", filepath.Base(src), err)
	}
	err = t.thumbnailAt(ctx, src, dst, t.lastFrameSeek(ctx, src)...)
	if !errors.Is(err, errNoOutput) {
		return err
	}
	return t.thumbnailAt(ctx, src, dst, "-ss", "0")
}

// lastFrameSeek are the input options that seek to the last frame of the first video stream:
// ffmpeg keeps the frames from the seek time on, so the time is one and a half frame periods
// before the end of the picture, which is the last frame and no earlier one. The stream's own
// length is used, as -sseof counts from the end of the container, which an audio track longer than
// the picture moves past the last frame; a stream that has no length of its own (Matroska) is as
// long as its container, so -sseof is right there.
func (t Tools) lastFrameSeek(ctx context.Context, src string) []string {
	info, err := t.Probe(ctx, src)
	if err != nil {
		return []string{"-sseof", "-0.1"}
	}
	back := 1.5 / info.FPS
	if info.VideoDuration <= 0 {
		return []string{"-sseof", strconv.FormatFloat(-back, 'f', 3, 64)}
	}
	return []string{"-ss", strconv.FormatFloat(max(info.VideoDuration-back, 0), 'f', 3, 64)}
}

// errNoOutput means ffmpeg succeeded and wrote no file.
var errNoOutput = errors.New("ffmpeg wrote no output")

// thumbnailAt writes one frame of src found by the input options seek.
func (t Tools) thumbnailAt(ctx context.Context, src, dst string, seek ...string) error {
	return t.write(ctx, dst, "image2", 0, append(append([]string{}, seek...),
		"-i", src, "-frames:v", "1", "-vf", "scale='min(960,iw)':-2", "-q:v", "3"))
}

// Proxy writes an H.264 copy of src that is at most 1080 pixels tall: what the preview plays
// instead of a 4K Take.
func (t Tools) Proxy(ctx context.Context, src, dst string) error {
	info, err := t.Probe(ctx, src)
	if err != nil {
		return err
	}
	return t.write(ctx, dst, "mp4", info.Duration, []string{
		"-i", src, "-vf", "scale=-2:'min(" + strconv.Itoa(MaxPreviewHeight) + ",ih)'",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart",
	})
}

// Frames writes the frames of src that the video filter keeps (at most maxFrames) as JPEGs named by
// the pattern (an ffmpeg output pattern such as dir/%04d.jpg); seconds is the length of src.
func (t Tools) Frames(ctx context.Context, src, pattern, filter string, maxFrames int, seconds float64) error {
	return t.runFFmpeg(ctx, t.limits().writeLimit(seconds), "-v", "error", "-i", src, "-an", "-vf", filter,
		"-frames:v", strconv.Itoa(maxFrames), "-q:v", "3", pattern)
}

// runFFmpeg runs ffmpeg with the arguments for at most limit. Cancelling ctx or running past the
// limit (ErrTimeout) returns that error as it is; a failure of ffmpeg carries its stderr.
func (t Tools) runFFmpeg(ctx context.Context, limit time.Duration, args ...string) error {
	_, stderr, err := run(ctx, limit, t.ffmpeg, args...)
	if err != nil && ctx.Err() == nil && !errors.Is(err, ErrTimeout) {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	return err
}

// tempPrefix and tempSuffix frame the name of the file write makes ffmpeg write before it renames it to dst.
const (
	tempPrefix = "."
	tempSuffix = ".ffmpeg.tmp"
)

// write runs ffmpeg with args, which handle seconds of media, and puts its output (of the given
// container format) at dst. The file appears only when ffmpeg succeeded, so a crash never leaves a
// half-written file at dst.
func (t Tools) write(ctx context.Context, dst, format string, seconds float64, args []string) error {
	dir, base := filepath.Split(dst)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp := filepath.Join(dir, tempPrefix+base+tempSuffix) // a crash leaves it behind: the next run to the same dst overwrites it, and fsutil.RemoveStaleTemp removes it at startup (a leading dot and a .tmp ending)
	full := append([]string{"-v", "error", "-y", "-nostdin"}, args...)
	full = append(full, "-f", format, tmp)
	if err := t.runFFmpeg(ctx, t.limits().writeLimit(seconds), full...); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if _, err := os.Stat(tmp); errors.Is(err, os.ErrNotExist) {
		return errNoOutput // ffmpeg exited 0 but wrote nothing (a seek past the last frame)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename output: %w", err)
	}
	return nil
}
