package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Video codecs Render writes.
const (
	CodecH264 = "h264"
	CodecH265 = "h265"
	CodecVP9  = "vp9"
)

// Cut is a part of a video file placed on the timeline of Render.
type Cut struct {
	Path       string
	Start, End float64
	// Transition is how the cut follows the previous one: "" or "cut" is a hard cut, otherwise
	// the name of an ffmpeg xfade transition ("fade", "fadeblack", "dissolve").
	Transition string
	// Overlap is the length of the transition in seconds. It is shortened to half of the shorter
	// of the two cuts it joins.
	Overlap float64
	// Caption is the text drawn over the cut ("" for none); ffmpeg needs the drawtext filter.
	Caption string
}

// Length is the duration of the cut in seconds.
func (c Cut) Length() float64 { return c.End - c.Start }

// RenderOptions choose the output of Render.
type RenderOptions struct {
	// Width and Height are the size of the picture; zero takes the size of the first cut. Cuts
	// of another shape are scaled to fit and centred on black.
	Width, Height int
	// Codec is CodecH264 (the default), CodecH265 or CodecVP9.
	Codec string
	// Audio is a finished audio file to lay under the picture. Empty keeps the sound of the cuts,
	// when every cut has some.
	Audio string
	// Draft trades size for speed (the preview of the Rough Cut).
	Draft bool
}

// Render cuts the segments out of their files and joins them in order with the transition each
// cut asks for (Edit Timeline of F-18 Simple), into one video at dst. The file appears at dst
// only when ffmpeg succeeded. A cut that runs past the end of its file is cut at the end of
// the file.
func (t Tools) Render(ctx context.Context, cuts []Cut, opt RenderOptions, dst string) (Info, error) {
	if len(cuts) == 0 {
		return Info{}, errors.New("render: no cut")
	}
	infos := make([]Info, len(cuts))
	clamped := make([]Cut, len(cuts))
	for i, c := range cuts {
		info, err := t.Probe(ctx, c.Path)
		if err != nil {
			return Info{}, err
		}
		c.End = min(c.End, info.Duration)
		if c.Start < 0 || c.Length() <= 0 {
			return Info{}, fmt.Errorf("render: cut %d is empty (%.3f-%.3f s of %.3f s)", i+1, c.Start, c.End, info.Duration)
		}
		infos[i], clamped[i] = info, c
	}
	w, h := opt.Width, opt.Height
	if w == 0 || h == 0 {
		w, h = infos[0].Width&^1, infos[0].Height&^1
	}
	var err error
	captionDir := ""
	captions := make([]string, len(clamped))
	for i, c := range clamped {
		if strings.TrimSpace(c.Caption) == "" {
			continue
		}
		if captionDir == "" {
			if captionDir, err = os.MkdirTemp("", "mam-caption-"); err != nil {
				return Info{}, fmt.Errorf("render: %w", err)
			}
			defer func() { _ = os.RemoveAll(captionDir) }()
		}
		if captions[i], err = captionFilter(captionDir, i, c.Caption, w, h); err != nil {
			return Info{}, fmt.Errorf("render: %w", err)
		}
	}
	graph, nativeAudio := renderGraph(clamped, infos, w, h, opt.Audio == "", captions)
	args := make([]string, 0, 2*len(clamped)+24)
	for _, c := range clamped {
		args = append(args, "-i", c.Path)
	}
	if opt.Audio != "" {
		args = append(args, "-i", opt.Audio)
	}
	args = append(args, "-filter_complex", graph, "-map", "[v]")
	switch {
	case opt.Audio != "":
		args = append(args, "-map", fmt.Sprintf("%d:a", len(clamped)), "-shortest")
	case nativeAudio:
		args = append(args, "-map", "[a]")
	}
	audioOut := opt.Audio != "" || nativeAudio
	codecArgs, format, err := codecArguments(opt.Codec, audioOut, opt.Draft)
	if err != nil {
		return Info{}, err
	}
	if err := t.write(ctx, dst, format, append(args, codecArgs...)); err != nil {
		return Info{}, err
	}
	return t.Probe(ctx, dst)
}

// codecArguments are the encoder arguments of the codec and the container format it goes in.
func codecArguments(codec string, audio, draft bool) (args []string, format string, err error) {
	switch codec {
	case "", CodecH264:
		preset, crf := "medium", "18"
		if draft {
			preset, crf = "veryfast", "23"
		}
		args = []string{"-c:v", "libx264", "-preset", preset, "-crf", crf, "-pix_fmt", "yuv420p", "-movflags", "+faststart"}
		format = "mp4"
		if audio {
			args = append(args, "-c:a", "aac", "-b:a", "192k")
		}
	case CodecH265:
		args = []string{"-c:v", "libx265", "-preset", "medium", "-crf", "22", "-x265-params", "log-level=error", "-tag:v", "hvc1", "-pix_fmt", "yuv420p", "-movflags", "+faststart"}
		format = "mp4"
		if audio {
			args = append(args, "-c:a", "aac", "-b:a", "192k")
		}
	case CodecVP9:
		args = []string{"-c:v", "libvpx-vp9", "-crf", "32", "-b:v", "0", "-row-mt", "1", "-pix_fmt", "yuv420p"}
		format = "webm"
		if audio {
			args = append(args, "-c:a", "libopus", "-b:a", "128k")
		}
	default:
		return nil, "", fmt.Errorf("render: unknown codec %q", codec)
	}
	if !audio {
		args = append(args, "-an")
	}
	return args, format, nil
}

// overlapOf is the transition length of cut i (0 for a hard cut and for the first cut).
func overlapOf(cuts []Cut, i int) float64 {
	if i == 0 || cuts[i].Transition == "" || cuts[i].Transition == "cut" {
		return 0
	}
	return min(cuts[i].Overlap, cuts[i-1].Length()/2, cuts[i].Length()/2)
}

// renderGraph builds the filter graph of Render: the picture comes out as [v], and, when native
// is set and every cut has sound, the sound as [a] (reported by the second result).
func renderGraph(cuts []Cut, infos []Info, w, h int, native bool, captions []string) (graph string, audio bool) {
	audio = native
	for _, in := range infos {
		audio = audio && in.HasAudio
	}
	var parts []string
	for i, c := range cuts {
		parts = append(parts, fmt.Sprintf(
			"[%d:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS,fps=%s,settb=AVTB,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p%s[v%d]",
			i, sec(c.Start), sec(c.End), infos[0].FrameRate, w, h, w, h, captions[i], i))
		if audio {
			parts = append(parts, fmt.Sprintf(
				"[%d:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo[a%d]",
				i, sec(c.Start), sec(c.End), i))
		}
	}
	if len(cuts) == 1 {
		parts = append(parts, "[v0]null[v]")
		if audio {
			parts = append(parts, "[a0]anull[a]")
		}
		return strings.Join(parts, ";"), audio
	}
	video, sound := "v0", "a0"
	elapsed := cuts[0].Length()
	for i := 1; i < len(cuts); i++ {
		outV, outA := fmt.Sprintf("x%d", i), fmt.Sprintf("y%d", i)
		if i == len(cuts)-1 {
			outV, outA = "v", "a"
		}
		if ov := overlapOf(cuts, i); ov > 0 {
			parts = append(parts, fmt.Sprintf("[%s][v%d]xfade=transition=%s:duration=%s:offset=%s[%s]", video, i, cuts[i].Transition, sec(ov), sec(elapsed-ov), outV))
			if audio {
				parts = append(parts, fmt.Sprintf("[%s][a%d]acrossfade=d=%s:c1=tri:c2=tri[%s]", sound, i, sec(ov), outA))
			}
			elapsed += cuts[i].Length() - ov
		} else {
			parts = append(parts, fmt.Sprintf("[%s][v%d]concat=n=2:v=1:a=0[%s]", video, i, outV))
			if audio {
				parts = append(parts, fmt.Sprintf("[%s][a%d]concat=n=2:v=0:a=1[%s]", sound, i, outA))
			}
			elapsed += cuts[i].Length()
		}
		video, sound = outV, outA
	}
	return strings.Join(parts, ";"), audio
}

// RenderedDuration is the length of the video Render makes from the cuts.
func RenderedDuration(cuts []Cut) float64 {
	total := 0.0
	for i, c := range cuts {
		total += c.Length() - overlapOf(cuts, i)
	}
	return total
}
