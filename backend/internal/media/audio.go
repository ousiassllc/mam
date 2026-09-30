package media

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
	"strconv"
	"strings"
)

// TruePeakCeiling is the loudest true peak (dBTP) the mastered mix may reach.
const TruePeakCeiling = -1.5

// ErrSilent means a file has no sound to measure.
var ErrSilent = errors.New("media: no sound")

// Loudness is what ffmpeg's loudnorm measures (EBU R128).
type Loudness struct {
	Integrated float64 // LUFS
	TruePeak   float64 // dBTP
	Range      float64 // LU
	Threshold  float64 // LUFS
	Offset     float64 // dB that reaches the target
}

// AudioDuration is the length of an audio (or any media) file in seconds.
func (t Tools) AudioDuration(ctx context.Context, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, t.ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path) // #nosec G204 -- fixed arguments; path is an asset file
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("ffprobe: %s has no duration", filepath.Base(path))
	}
	return d, nil
}

// loudnormJSON is the report loudnorm prints; every number is a string.
type loudnormJSON struct {
	InputI      string `json:"input_i"`
	InputTP     string `json:"input_tp"`
	InputLRA    string `json:"input_lra"`
	InputThresh string `json:"input_thresh"`
	Offset      string `json:"target_offset"`
}

// MeasureLoudness measures the integrated loudness, true peak and range of the sound of the file
// against the given target (the target only steers the reported Offset). ErrSilent when the file
// has no measurable sound.
func (t Tools) MeasureLoudness(ctx context.Context, path string, targetLUFS float64) (Loudness, error) {
	filter := fmt.Sprintf("loudnorm=I=%s:TP=%s:LRA=11:print_format=json", num(targetLUFS), num(TruePeakCeiling))
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, t.ffmpeg, "-hide_banner", "-nostats", "-nostdin", "-i", path, "-vn", "-af", filter, "-f", "null", "-") // #nosec G204 -- arguments are built by this package; path is an asset file
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Loudness{}, ctx.Err()
		}
		return Loudness{}, fmt.Errorf("ffmpeg loudnorm: %w: %s", err, tail(stderr.String()))
	}
	text := stderr.String()
	start, end := strings.LastIndex(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return Loudness{}, fmt.Errorf("ffmpeg loudnorm: no report: %s", tail(text))
	}
	var r loudnormJSON
	if err := json.Unmarshal([]byte(text[start:end+1]), &r); err != nil {
		return Loudness{}, fmt.Errorf("ffmpeg loudnorm: read report: %w", err)
	}
	l := Loudness{Integrated: parseDB(r.InputI), TruePeak: parseDB(r.InputTP), Range: parseDB(r.InputLRA), Threshold: parseDB(r.InputThresh), Offset: parseDB(r.Offset)}
	if math.IsInf(l.Integrated, 0) || math.IsNaN(l.Integrated) {
		return Loudness{}, ErrSilent
	}
	return l, nil
}

// parseDB reads a number of the report; "-inf" is negative infinity.
func parseDB(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return math.Inf(-1)
	}
	return v
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[len(s)-400:]
	}
	return s
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// Track is a sound to mix in, starting at Start seconds.
type Track struct {
	Path  string
	Start float64
	// Length is how long the sound plays (0: the whole file). A Loop track needs it.
	Length float64
	// Loop repeats a shorter file until Length.
	Loop bool
	// GainDB is the level relative to the file.
	GainDB float64
	// Fade fades the sound in and out over that many seconds (shortened to a quarter of Length).
	Fade float64
}

// chain is the filter chain that shapes the track before it is placed: trim, fade, level, delay.
func (tr Track) chain() string {
	var f []string
	if tr.Length > 0 {
		f = append(f, "atrim=duration="+sec(tr.Length))
		if fade := min(tr.Fade, tr.Length/4); fade > 0 {
			f = append(f, "afade=t=in:d="+sec(fade), "afade=t=out:st="+sec(tr.Length-fade)+":d="+sec(fade))
		}
	}
	if tr.GainDB != 0 {
		f = append(f, "volume="+num(tr.GainDB)+"dB")
	}
	if ms := int(math.Round(max(tr.Start, 0) * 1000)); ms > 0 {
		f = append(f, fmt.Sprintf("adelay=%d|%d", ms, ms))
	}
	if len(f) == 0 {
		return ""
	}
	return "," + strings.Join(f, ",")
}

// PlaceVoices writes a mono WAV of the given length with every voice at its Start and silence
// around them: the audio a Lip Sync of a clip follows.
func (t Tools) PlaceVoices(ctx context.Context, voices []Track, seconds float64, dst string) error {
	if len(voices) == 0 {
		return errors.New("place voices: no voice")
	}
	var args, parts, labels []string
	for i, v := range voices {
		v.Length, v.Loop, v.GainDB, v.Fade = 0, false, 0, 0
		args = append(args, "-i", v.Path)
		parts = append(parts, fmt.Sprintf("[%d:a]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=mono%s[v%d]", i, v.chain(), i))
		labels = append(labels, fmt.Sprintf("[v%d]", i))
	}
	mix := strings.Join(labels, "") + fmt.Sprintf("amix=inputs=%d:duration=longest:normalize=0,apad=whole_dur=%s,atrim=end=%s[out]", len(labels), sec(seconds), sec(seconds))
	graph := strings.Join(append(parts, mix), ";")
	return t.write(ctx, dst, "wav", append(args, "-filter_complex", graph, "-map", "[out]", "-c:a", "pcm_s16le"))
}

// Master mixes the sound of base (a video file; "" for none) and the tracks into one stereo track
// of the given length, normalizes it to targetLUFS with a two-pass loudnorm (local DSP) and writes
// it as AAC at dst. It returns the loudness of the written file. ErrSilent when there is no sound
// at all.
func (t Tools) Master(ctx context.Context, base string, tracks []Track, seconds, targetLUFS float64, dst string) (Loudness, error) {
	var args, parts, labels []string
	add := func(path, chain string, loop bool) {
		i := len(labels)
		if loop {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", path)
		parts = append(parts, fmt.Sprintf("[%d:a]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo%s[m%d]", i, chain, i))
		labels = append(labels, fmt.Sprintf("[m%d]", i))
	}
	if base != "" {
		add(base, "", false)
	}
	for _, tr := range tracks {
		if tr.Loop && tr.Length <= 0 {
			return Loudness{}, fmt.Errorf("master: looped track %s has no length", filepath.Base(tr.Path))
		}
		add(tr.Path, tr.chain(), tr.Loop)
	}
	if len(labels) == 0 {
		return Loudness{}, ErrSilent
	}
	mix := strings.Join(labels, "") + fmt.Sprintf("amix=inputs=%d:duration=longest:normalize=0,apad=whole_dur=%s,atrim=end=%s[out]", len(labels), sec(seconds), sec(seconds))
	graph := strings.Join(append(parts, mix), ";")

	dir, name := filepath.Split(dst)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Loudness{}, fmt.Errorf("create directory: %w", err)
	}
	raw := filepath.Join(dir, "."+name+".mix.tmp") // an interrupted run leaves it behind; the next run to the same dst overwrites it
	defer func() { _ = os.Remove(raw) }()
	if err := t.write(ctx, raw, "wav", append(args, "-filter_complex", graph, "-map", "[out]", "-c:a", "pcm_f32le")); err != nil {
		return Loudness{}, err
	}
	first, err := t.MeasureLoudness(ctx, raw, targetLUFS)
	if err != nil {
		return Loudness{}, err
	}
	norm := fmt.Sprintf("loudnorm=I=%s:TP=%s:LRA=11:measured_I=%s:measured_TP=%s:measured_LRA=%s:measured_thresh=%s:offset=%s:linear=true",
		num(targetLUFS), num(TruePeakCeiling), num(first.Integrated), num(first.TruePeak), num(first.Range), num(first.Threshold), num(first.Offset))
	if err := t.write(ctx, dst, "mp4", []string{"-i", raw, "-af", norm + ",aresample=48000,aformat=channel_layouts=stereo", "-c:a", "aac", "-b:a", "192k", "-vn"}); err != nil {
		return Loudness{}, err
	}
	return t.MeasureLoudness(ctx, dst, targetLUFS)
}
