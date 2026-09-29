// Package media runs ffmpeg and ffprobe on the video files of Takes: it probes them, makes the
// poster frame and the 1080p proxy of a 4K Take (spec/requirements/non-functional.md §2) and cuts
// Takes together with a cross-fade at every joint (Partial Take Use, spec/spec.md §14).
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrUnavailable means ffmpeg / ffprobe are not installed.
var ErrUnavailable = errors.New("media: ffmpeg and ffprobe are required")

// MaxPreviewHeight is the tallest picture the preview plays as it is; a taller Take (4K) is
// shown through its proxy.
const MaxPreviewHeight = 1080

// Tools is the ffmpeg and ffprobe executables.
type Tools struct{ ffmpeg, ffprobe string }

// Look finds ffmpeg and ffprobe on the PATH. It returns ErrUnavailable when either is missing.
func Look() (Tools, error) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		return Tools{}, ErrUnavailable
	}
	return Tools{ffmpeg: ffmpeg, ffprobe: ffprobe}, nil
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
	cmd := exec.CommandContext(ctx, t.ffprobe, "-v", "error", // #nosec G204 -- fixed arguments; path is an asset file
		"-show_entries", "stream=codec_type,r_frame_rate,duration,width,height:format=duration", "-of", "json", path)
	out, err := cmd.Output()
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	var info Info
	videoDuration := 0.0
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if info.Width != 0 {
				continue // the first video stream only
			}
			info.Width, info.Height = s.Width, s.Height
			info.FrameRate = s.RFrameRate
			info.FPS = parseRate(s.RFrameRate)
			videoDuration, _ = strconv.ParseFloat(s.Duration, 64)
		case "audio":
			info.HasAudio = true
		}
	}
	if info.Width == 0 {
		return Info{}, fmt.Errorf("ffprobe: %s has no video stream", filepath.Base(path))
	}
	if info.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64); info.Duration <= 0 {
		info.Duration = videoDuration
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
// 960 pixels wide.
func (t Tools) Thumbnail(ctx context.Context, src, dst string, at float64) error {
	return t.write(ctx, dst, "image2", []string{
		"-ss", strconv.FormatFloat(max(at, 0), 'f', 3, 64), "-i", src,
		"-frames:v", "1", "-vf", "scale='min(960,iw)':-2", "-q:v", "3",
	})
}

// Proxy writes an H.264 copy of src that is at most 1080 pixels tall: what the preview plays
// instead of a 4K Take.
func (t Tools) Proxy(ctx context.Context, src, dst string) error {
	return t.write(ctx, dst, "mp4", []string{
		"-i", src, "-vf", "scale=-2:'min(" + strconv.Itoa(MaxPreviewHeight) + ",ih)'",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart",
	})
}

// write runs ffmpeg with args and puts its output (of the given container format) at dst. The file
// appears only when ffmpeg succeeded, so a crash never leaves a half-written file at dst.
func (t Tools) write(ctx context.Context, dst, format string, args []string) error {
	dir, base := filepath.Split(dst)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp := filepath.Join(dir, "."+base+".ffmpeg.tmp") // the stale-temp sweep at startup removes it
	full := append([]string{"-v", "error", "-y", "-nostdin"}, args...)
	full = append(full, "-f", format, tmp)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, t.ffmpeg, full...) // #nosec G204 -- arguments are built by this package; paths are asset files
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename output: %w", err)
	}
	return nil
}
