// Package audioinput is the audio file of the generation test's video form (Issue #1153): the file input of a speech-to-video model (Wan 2.2 S2V), the row
// of an audio input that is already chosen, and the player of one in a result. It is a package of its own so that organisms/gentest stays inside its line
// limit. The audio is one more input of the batch with the role "audio" (service/gentest/audioin and audiofile have the rules: WAV, MP3, FLAC, OGG or M4A, one per batch).
package audioinput

import "github.com/ousiassllc/mam/backend/internal/audiofile"

// The names the form and the handlers share with organisms/gentest (FieldAudio is Field; the handler tests drive Role through the service's role).
const (
	// Field is the multipart field of the file; refform stages it as an input with the role Role.
	Field = "upload_audio"
	// Role is the role of the audio input, as the form posts it (role_<ref>) and the service names it.
	Role = "audio"
	// MaxMiB is the largest file the form says it takes (audiofile.MaxBytes in MiB).
	MaxMiB = audiofile.MaxBytes >> 20
	// InputID is the id of the file input: the notice of the input check listens for its change.
	InputID = "gt-audio"
)

// Accept is the accept of the file input: the kinds the service takes, by type and by extension (a browser may not know the type of a .flac).
var Accept = audiofile.Accept()
