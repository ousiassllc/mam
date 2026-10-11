package media

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// FadeSeconds is the length of the cross-fade at a joint between two segments. It is shortened
// when a segment is too short to give it up (see Compose).
const FadeSeconds = 0.25

// Segment is the part of a video file from Start to End seconds.
type Segment struct {
	Path       string
	Start, End float64
}

// Length is the duration of the segment in seconds.
func (s Segment) Length() float64 { return s.End - s.Start }

// Compose cuts the segments out of their files and joins them, in order, into one video at dst.
// The joints are cross-faded (the picture, and the sound when every file has some) so that the cut
// does not jump: this is the boundary handling of Partial Take Use ("Take A の 0〜2 秒 + Take B の
// 2〜4 秒"). The picture takes the size and frame rate of the first segment; the others are scaled
// to fit. A segment that runs past the end of its file is cut at the end of the file.
func (t Tools) Compose(ctx context.Context, segs []Segment, dst string) (Info, error) {
	if len(segs) == 0 {
		return Info{}, errors.New("compose: no segment")
	}
	infos := make([]Info, len(segs))
	cut := make([]Segment, len(segs))
	seconds := 0.0
	for i, s := range segs {
		info, err := t.Probe(ctx, s.Path)
		if err != nil {
			return Info{}, err
		}
		s.End = min(s.End, info.Duration)
		if s.Start < 0 || s.Length() <= 0 {
			return Info{}, fmt.Errorf("compose: segment %d is empty (%.3f-%.3f s of %.3f s)", i+1, s.Start, s.End, info.Duration)
		}
		infos[i], cut[i] = info, s
		seconds += s.Length()
	}
	graph, audio := composeGraph(cut, infos)
	args := make([]string, 0, 2*len(cut)+16)
	for _, s := range cut {
		args = append(args, "-i", s.Path)
	}
	args = append(args, "-filter_complex", graph, "-map", "[v]")
	if audio {
		args = append(args, "-map", "[a]", "-c:a", "aac", "-b:a", "192k")
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-c:v", "libx264", "-preset", "medium", "-crf", "18", "-pix_fmt", "yuv420p", "-movflags", "+faststart")
	if err := t.write(ctx, dst, "mp4", seconds, args); err != nil {
		return Info{}, err
	}
	return t.Probe(ctx, dst)
}

// fadeFor is the cross-fade length for the segments: FadeSeconds, or a third of the shortest
// segment when that is shorter. One segment has no joint.
func fadeFor(segs []Segment) float64 {
	if len(segs) < 2 {
		return 0
	}
	shortest := segs[0].Length()
	for _, s := range segs[1:] {
		shortest = min(shortest, s.Length())
	}
	return min(FadeSeconds, shortest/3)
}

// composeGraph builds the filter graph of Compose and reports whether it produces sound ([a]).
// The picture comes out as [v].
func composeGraph(segs []Segment, infos []Info) (graph string, audio bool) {
	first := infos[0]
	w, h := first.Width&^1, first.Height&^1
	audio = true
	for _, in := range infos {
		audio = audio && in.HasAudio
	}
	fade := fadeFor(segs)
	var parts []string
	for i, s := range segs {
		parts = append(parts, fmt.Sprintf(
			"[%d:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS,fps=%s,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p[v%d]",
			i, sec(s.Start), sec(s.End), first.FrameRate, w, h, w, h, i))
		if audio {
			parts = append(parts, fmt.Sprintf(
				"[%d:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo[a%d]",
				i, sec(s.Start), sec(s.End), i))
		}
	}
	if len(segs) == 1 {
		parts = append(parts, "[v0]null[v]")
		if audio {
			parts = append(parts, "[a0]anull[a]")
		}
		return strings.Join(parts, ";"), audio
	}
	video, sound := "v0", "a0"
	elapsed := segs[0].Length()
	for i := 1; i < len(segs); i++ {
		outV, outA := fmt.Sprintf("x%d", i), fmt.Sprintf("y%d", i)
		if i == len(segs)-1 {
			outV, outA = "v", "a"
		}
		// The joint starts one fade before the end of what has been joined so far.
		parts = append(parts, fmt.Sprintf("[%s][v%d]xfade=transition=fade:duration=%s:offset=%s[%s]", video, i, sec(fade), sec(elapsed-fade), outV))
		if audio {
			parts = append(parts, fmt.Sprintf("[%s][a%d]acrossfade=d=%s:c1=tri:c2=tri[%s]", sound, i, sec(fade), outA))
		}
		video, sound = outV, outA
		elapsed += segs[i].Length() - fade
	}
	return strings.Join(parts, ";"), audio
}

// JoinedDuration is the length of the video Compose makes from the segments.
func JoinedDuration(segs []Segment) float64 {
	total := 0.0
	for _, s := range segs {
		total += s.Length()
	}
	return total - float64(max(len(segs)-1, 0))*fadeFor(segs)
}

func sec(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
