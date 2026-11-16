// Package audiopanel draws the audio of a Shot in the Shot Inspector (Issue #1153): the sound a speech-to-video model (Wan 2.2 S2V) makes the Shot's clip from, with the
// form that ties an audio asset of the Project to the Shot and the one that takes it off. It answers PUT /shots/:id/audio with its own section, so that what is typed in the
// other forms of the Inspector is kept. It is a package of its own, below shotinspector, so that the Inspector's data can hold it.
package audiopanel

import (
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ID is the id of the section: the target of its forms.
const ID = "shot-audio"

// Data is the audio of one Shot as the Inspector shows it. The zero Data (no ShotID) draws nothing.
type Data struct {
	ShotID    string
	ProjectID string
	// Unavailable is true when the audio could not be read: the section says so and offers nothing.
	Unavailable bool
	// Current is the audio the Shot is tied to; nil when it has none.
	Current *Current
	// Choices are the audio assets of the Project the Shot can be tied to, newest first.
	Choices []Choice
	// Locked (the Prompt is LOCKed) and Busy (the Shot is generated or repaired) draw the section read-only, with the reason.
	Locked, Busy bool
	// MaxSeconds is the longest sound a model takes; Pictures the images (Keyframe, Reference Pack) the Shot has.
	MaxSeconds, Pictures int
}

// Current is the audio a Shot is tied to. Problem is why a generation cannot use it ("" when it can).
type Current struct {
	AssetID, MimeType string
	SizeBytes         int64
	Seconds           float64
	Problem           string
}

// Choice is an audio asset of the Project. An asset has no name, so the option says when it came in, what kind of file it is and how big.
type Choice struct {
	AssetID, MimeType string
	SizeBytes         int64
	CreatedAt         time.Time
}

// Formats is the list of the audio files the Project takes ("WAV・MP3・FLAC・OGG・M4A"), for the hint that says how to get one in.
func Formats() string {
	var names []string
	for _, ext := range []string{audiofile.WAV, audiofile.MP3, audiofile.FLAC, audiofile.OGG, audiofile.M4A} {
		names = append(names, kind(ext))
	}
	return strings.Join(names, "・")
}

// MaxMiB is the largest audio file the Project takes, in MiB.
const MaxMiB = audiofile.MaxBytes >> 20

// kind is the name of a kind of file ("WAV"): its extension in capitals; "" when mime is not an audio kind.
func kind(mime string) string {
	return strings.ToUpper(strings.TrimPrefix(audiofile.Ext(mime), "."))
}

// facts is "WAV · 3.2 秒 · 480 KB" (the parts that are known).
func (c Current) facts() string {
	parts := []string{}
	if k := kind(c.MimeType); k != "" {
		parts = append(parts, k)
	}
	if c.Seconds > 0 {
		parts = append(parts, fmt.Sprintf("%.1f 秒", c.Seconds))
	}
	if c.SizeBytes > 0 {
		parts = append(parts, domain.FormatBytes(c.SizeBytes))
	}
	return strings.Join(parts, " · ")
}

// Label is the option of the select: "2026-11-16 12:34 · WAV · 480 KB".
func (c Choice) Label() string {
	parts := []string{c.CreatedAt.Local().Format("2006-01-02 15:04")}
	if k := kind(c.MimeType); k != "" {
		parts = append(parts, k)
	}
	if c.SizeBytes > 0 {
		parts = append(parts, domain.FormatBytes(c.SizeBytes))
	}
	return strings.Join(parts, " · ")
}

// hasPlayer reports whether the tied asset can be played: its row exists (an asset that is gone has no kind).
func (c Current) hasPlayer() bool { return kind(c.MimeType) != "" }

// readOnlyReason is why the forms are not drawn ("" when they are).
func (d Data) readOnlyReason() string {
	switch {
	case d.Busy:
		return "生成・Repair 中は音声を変えられません。終わってから開き直してください。"
	case d.Locked:
		return "Prompt が LOCK されているため、音声を変えられません。LOCK を解除してから変更してください。"
	}
	return ""
}

// limitText is "音声の長さの上限は 60 秒です。".
func (d Data) limitText() string {
	if d.MaxSeconds <= 0 {
		return ""
	}
	return fmt.Sprintf("音声の長さの上限は %d 秒です（設定の gentest.max_audio_seconds）。", d.MaxSeconds)
}

// needsPicture reports whether the Shot is tied to audio but has no image for the model to draw the speaker from.
func (d Data) needsPicture() bool { return d.Current != nil && d.Pictures == 0 }
