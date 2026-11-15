// Package audioin is the audio file a generation test takes as an input (Issue #1153; spec/architecture/gentest/gentest.md "音声の入力"): the sound a
// speech-to-video model such as Wan 2.2 S2V makes the clip from. It is a role of the input list ("audio", gentestrepo.RoleAudio), one per batch, and
// not an image: the types, the limit and the check of the bytes are here, so that package gentest, the web form and the MCP tools say the same.
//
// Only WAV and MP3 are taken. The kind is read from the bytes (never from the name or the type the client sent), and the stored file keeps the extension of
// its kind: ComfyUI lists the audio files of its input folder by the extension (LoadAudio), so the extension is part of the contract with the engine.
package audioin

import (
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// MaxBytes is the largest audio file (50 MiB: a few minutes of 16-bit stereo WAV; a clip of a video model is seconds long, and the model reads the start of it).
const MaxBytes = 50 << 20

// The two kinds, with the extension the stored file and the engine's upload take.
const (
	WAV = "audio/wav"
	MP3 = "audio/mpeg"
)

var extensions = map[string]string{WAV: ".wav", MP3: ".mp3"}

// Is reports whether mime is the type of an audio file a batch takes.
func Is(mime string) bool { _, ok := extensions[mime]; return ok }

// Ext is the extension of the stored file of an audio type ("" for any other type).
func Ext(mime string) string { return extensions[mime] }

// MimeOfExt is the audio type of a stored file's extension (".wav", ".mp3"); false for any other.
func MimeOfExt(ext string) (string, bool) {
	for mime, e := range extensions {
		if e == ext {
			return mime, true
		}
	}
	return "", false
}

// Sniff reads the kind of an audio file from its first bytes: "RIFF....WAVE" is WAV; an ID3 tag or an MPEG audio frame header (layer III, a valid
// bitrate and sample rate) is MP3. false for anything else (an image, a text, a FLAC, an AAC...).
func Sniff(data []byte) (mime string, ok bool) {
	switch {
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return WAV, true
	case len(data) >= 3 && string(data[0:3]) == "ID3":
		return MP3, true
	case len(data) >= 4 && data[0] == 0xFF && data[1]&0xE0 == 0xE0 && data[1]>>3&3 != 1 && data[1]>>1&3 == 1 && data[2]>>4 != 0xF && data[2]>>2&3 != 3:
		return MP3, true
	}
	return "", false
}

// Stage stores the uploaded bytes in dir (created) as a staged file named by a new id (newID) and the extension of their kind, and returns the id and the
// type. The bytes must be a WAV or an MP3 of 1 byte to MaxBytes; the kind is read from the bytes (Sniff), not from anything the client said.
func Stage(dir string, data []byte, newID func() string) (id, mime string, err error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return "", "", gterr.Invalid("音声ファイルは 1 バイト以上 %d MiB 以下にしてください", MaxBytes>>20)
	}
	mime, ok := Sniff(data)
	if !ok {
		return "", "", gterr.Invalid("音声ファイルは WAV か MP3 にしてください")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", err
	}
	id = newID() + Ext(mime)
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, id), data, 0o600); err != nil {
		return "", "", err
	}
	return id, mime, nil
}
