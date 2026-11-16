// Package audiofile knows the audio files a generation test takes as an input (Issue #1153; spec/architecture/gentest/gentest.md "音声の入力"): WAV, MP3, FLAC,
// OGG (Vorbis or Opus) and M4A (AAC in an MP4 container). It has no dependency on the gentest packages: the kind of a file is read from its bytes (Sniff),
// and so is its length (Duration), in pure Go, because the length of the sound is the length of the clip a speech-to-video model makes (no ffprobe).
//
// The reader trusts nothing: every loop advances by at least one box, chunk, page or frame and is bounded by the length of the data, so truncated or
// corrupt bytes end in an error, never in a panic or an endless loop.
package audiofile

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxBytes is the largest audio file (50 MiB: a few minutes of 16-bit stereo WAV).
const MaxBytes = 50 << 20

// The five kinds, as the media type the file is stored and uploaded under.
const (
	WAV  = "audio/wav"
	MP3  = "audio/mpeg"
	FLAC = "audio/flac"
	OGG  = "audio/ogg"
	M4A  = "audio/mp4"
)

// kinds lists the kinds with the extension of the stored file: ComfyUI lists the audio files of its input folder by the extension (LoadAudio), so the
// extension is part of the contract with the engine.
var kinds = []struct{ mime, ext string }{{WAV, ".wav"}, {MP3, ".mp3"}, {FLAC, ".flac"}, {OGG, ".ogg"}, {M4A, ".m4a"}}

// ErrUnreadable is what Duration returns for bytes that are not an audio file of one of the kinds, or whose length cannot be read (a header that is cut or
// corrupt, a stream whose length is not written, a length of zero). The error wraps it with the reason.
var ErrUnreadable = errors.New("audiofile: the length of the audio cannot be read")

// Accept lists the types and then the extensions of the kinds, comma separated, as the accept attribute of a file input takes them (a browser may not know
// the type of a .flac or .m4a, so the extension is there too).
func Accept() string {
	var types, exts []string
	for _, k := range kinds {
		types, exts = append(types, k.mime), append(exts, k.ext)
	}
	return strings.Join(append(types, exts...), ",")
}

// Is reports whether mime is the type of an audio file a batch takes.
func Is(mime string) bool { return Ext(mime) != "" }

// Ext is the extension of the stored file of an audio type ("" for any other type).
func Ext(mime string) string {
	for _, k := range kinds {
		if k.mime == mime {
			return k.ext
		}
	}
	return ""
}

// MimeOfExt is the audio type of a stored file's extension (".wav", ".mp3", ".flac", ".ogg", ".m4a"); false for any other (the letter case matters).
func MimeOfExt(ext string) (string, bool) {
	for _, k := range kinds {
		if k.ext == ext {
			return k.mime, true
		}
	}
	return "", false
}

// maxLength is the longest length a file may say (a year): above it the field is a corrupt one, and the arithmetic of a time.Duration stays far from overflow.
const maxLength = maxSeconds * time.Second

// maxSeconds is maxLength in whole seconds.
const maxSeconds = 366 * 24 * 60 * 60

// lengthOf is count / rate seconds as a Duration; false when rate is 0 or the length is not within (0, maxLength]. It never overflows.
func lengthOf(count, rate uint64) (time.Duration, bool) {
	if rate == 0 || count == 0 {
		return 0, false
	}
	secs := count / rate
	if secs > maxSeconds {
		return 0, false
	}
	ns := secs*uint64(time.Second) + (count%rate)*uint64(time.Second)/rate // at most maxLength: no overflow
	if ns == 0 || ns > uint64(maxLength) {
		return 0, false
	}
	return time.Duration(ns), true
}

// u64 is n as a uint64, for a length or an offset: 0 for a negative n, which none of them is.
func u64(n int) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}

// lengthOrErr is lengthOf as a (Duration, error) pair, for the readers.
func lengthOrErr(count, rate uint64) (time.Duration, error) {
	d, ok := lengthOf(count, rate)
	if !ok {
		return 0, unreadable("the length is zero or out of range")
	}
	return d, nil
}

// unreadable is ErrUnreadable with the reason.
func unreadable(reason string) error { return fmt.Errorf("%w (%s)", ErrUnreadable, reason) }
