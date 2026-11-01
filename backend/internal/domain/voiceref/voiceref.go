// Package voiceref is the reference voice of a Character (F-20, F-29): the short recording a local voice
// provider (GPT-SoVITS) clones so that the same Character speaks with the same voice in every Episode. It is
// a sub-package of domain, which is at its line limit; domain.WorldCharacter holds it as VoiceReference.
package voiceref

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of a reference voice. The duration is what GPT-SoVITS asks of the reference audio (3 to 10 seconds).
const (
	// MaxBytes is the largest recording: the same limit as the sample of a cloned voice (service.MaxVoiceSampleBytes).
	MaxBytes = 10 << 20
	// MinDuration and MaxDuration bound the length of the recording.
	MinDuration = 3 * time.Second
	MaxDuration = 10 * time.Second
	// MaxTextRunes bounds the transcript of the recording.
	MaxTextRunes = 500
	// DefaultLanguage is the language of a reference voice that does not say.
	DefaultLanguage = "ja"
	// NarrationName is the name of the Character that stands for the narration: its reference voice is the
	// voice of the narrator (there is no asset kind of its own).
	NarrationName = "ナレーション"
)

// Reference is the reference voice of one Character row. FilePath is relative to the assets directory; the
// file is shared by the copies of the row (the canon and the Episodes), like the canon images.
type Reference struct {
	FilePath  string `json:"file_path"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	// DurationMS is the measured length of the recording; 0 when ffprobe was not available to measure it.
	DurationMS int `json:"duration_ms,omitempty"`
	// Text is what the recording says (GPT-SoVITS uses the transcript of the reference audio); "" when not given.
	Text     string `json:"text,omitempty"`
	Language string `json:"language"`
}

// Value stores the reference as its JSON text (the character.voice_reference column).
func (r Reference) Value() (driver.Value, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode voice reference: %w", err)
	}
	return string(raw), nil
}

// Scan reads the JSON text of the character.voice_reference column; a NULL leaves nothing to scan
// (database/sql sets a nil *Reference without calling Scan).
func (r *Reference) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("scan voice reference: unsupported type %T", src)
	}
	if err := json.Unmarshal(raw, r); err != nil {
		return fmt.Errorf("decode voice reference: %w", err)
	}
	return nil
}

// Duration is the measured length of the recording; 0 when it was not measured.
func (r Reference) Duration() time.Duration { return time.Duration(r.DurationMS) * time.Millisecond }

// Format is an audio format a reference voice may have.
type Format struct {
	Extension string
	MimeType  string
	// matches tells whether the head of the file is that format: the content, not the name, decides.
	matches func(head []byte) bool
}

// formats are the audio formats GPT-SoVITS reads; the signature of the file must match its extension.
var formats = []Format{
	{".wav", "audio/wav", func(h []byte) bool {
		return len(h) >= 12 && bytes.HasPrefix(h, []byte("RIFF")) && bytes.Equal(h[8:12], []byte("WAVE"))
	}},
	{".mp3", "audio/mpeg", func(h []byte) bool {
		return bytes.HasPrefix(h, []byte("ID3")) || len(h) >= 2 && h[0] == 0xFF && h[1]&0xE0 == 0xE0 // an ID3 tag or a frame sync
	}},
	{".flac", "audio/flac", func(h []byte) bool { return bytes.HasPrefix(h, []byte("fLaC")) }},
	{".ogg", "audio/ogg", func(h []byte) bool { return bytes.HasPrefix(h, []byte("OggS")) }},
	{".m4a", "audio/mp4", func(h []byte) bool { return len(h) >= 12 && bytes.Equal(h[4:8], []byte("ftyp")) }},
}

// FormatOf returns the format of the file extension (any letter case); ok is false for an extension that is not one.
func FormatOf(ext string) (Format, bool) {
	ext = strings.ToLower(ext)
	i := slices.IndexFunc(formats, func(f Format) bool { return f.Extension == ext })
	if i < 0 {
		return Format{}, false
	}
	return formats[i], true
}

// Matches reports whether head (the first bytes of the file) has the signature of the format.
func (f Format) Matches(head []byte) bool { return f.matches(head) }

// Extensions lists the extensions of the supported formats, for the message and the file input.
func Extensions() []string {
	out := make([]string, len(formats))
	for i, f := range formats {
		out[i] = f.Extension
	}
	return out
}

// Languages are the languages of a reference voice (the ones GPT-SoVITS speaks): Japanese, English, Chinese,
// Korean and Cantonese.
func Languages() []string { return []string{"ja", "en", "zh", "ko", "yue"} }

// ErrLanguage and ErrText are what NormalizeLanguage and NormalizeText refuse a value with.
var (
	ErrLanguage = errors.New("voiceref: unknown language")
	ErrText     = errors.New("voiceref: text is too long")
)

// NormalizeLanguage returns the language code, "ja" for an empty one; ErrLanguage for one that is not in Languages.
func NormalizeLanguage(lang string) (string, error) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return DefaultLanguage, nil
	}
	if !slices.Contains(Languages(), lang) {
		return "", ErrLanguage
	}
	return lang, nil
}

// NormalizeText trims the transcript; ErrText when it is longer than MaxTextRunes.
func NormalizeText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > MaxTextRunes {
		return "", ErrText
	}
	return text, nil
}
