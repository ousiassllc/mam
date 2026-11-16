// Package audioin is the audio file a generation test takes as an input (Issue #1153; spec/architecture/gentest/gentest.md "音声の入力"): the sound a
// speech-to-video model such as Wan 2.2 S2V makes the clip from. It is a role of the input list ("audio", gentestrepo.RoleAudio), one per batch, and
// not an image. The kinds, the limit and the check of the bytes are package audiofile's (WAV, MP3, FLAC, OGG, M4A), so that the web form, the MCP tools,
// the staging and the engine say the same; what is here is the part that belongs to a generation test: the staging of an upload and the words it is
// refused with.
//
// The kind is read from the bytes (never from the name or the type the client sent), and the stored file keeps the extension of its kind: ComfyUI lists
// the audio files of its input folder by the extension (LoadAudio), so the extension is part of the contract with the engine.
package audioin

import (
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// KindsText names the kinds in the sentences a refusal says, so that every message of every entrance (form, MCP, staging, check) says the same.
const KindsText = "WAV・MP3・FLAC・OGG・M4A"

// NotAudioMessage is what a file that is none of the kinds is refused with. An MP4 video is refused
// with it too: the brands "mp42" and "isom" are taken only for a file whose tracks are all sound (audiofile.Sniff).
const NotAudioMessage = "音声ファイルは " + KindsText + " のどれかにしてください"

// Stage stores the uploaded bytes in dir (created) as a staged file named by a new id (newID) and the extension of their kind, and returns the id and the
// type. The bytes must be an audio file of one of the kinds of 1 byte to audiofile.MaxBytes; the kind is read from the bytes (audiofile.Sniff), not from
// anything the client said.
func Stage(dir string, data []byte, newID func() string) (id, mime string, err error) {
	if len(data) == 0 || len(data) > audiofile.MaxBytes {
		return "", "", gterr.Invalid("音声ファイルは 1 バイト以上 %d MiB 以下にしてください", audiofile.MaxBytes>>20)
	}
	mime, ok := audiofile.Sniff(data)
	if !ok {
		return "", "", gterr.Invalid("%s", NotAudioMessage)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", err
	}
	id = newID() + audiofile.Ext(mime)
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, id), data, 0o600); err != nil {
		return "", "", err
	}
	return id, mime, nil
}
