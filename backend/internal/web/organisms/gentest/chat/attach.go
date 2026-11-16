package chat

import (
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/audioinput"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The form of an attachment (Issue #1153): the sound and the picture a speech-to-video model needs, which the user attaches to the conversation and the LLM can only
// use ("audio": true / "image": true in an estimate). The handlers read the same names.
const (
	// FieldKind is KindAudioFile or KindImageFile; FieldFile is the file.
	FieldKind = "kind"
	FieldFile = "file"
	// KindAudioFile and KindImageFile are the values of FieldKind (the service's AttachAudio and AttachImage).
	KindAudioFile = "audio"
	KindImageFile = "image"
	// MaxImageMiB is the largest picture the form says it takes (gentest.MaxImageBytes in MiB, which the handlers' test holds equal).
	MaxImageMiB = 20
	// ImageAccept is the accept of the picture input: the types the service takes.
	ImageAccept = "image/png,image/jpeg,image/webp,.png,.jpg,.jpeg,.webp"
)

// AudioAccept is the accept of the sound input and AudioMaxMiB its limit (package audioinput's, so the form of the generation test and the chat say the same).
var AudioAccept = audioinput.Accept

// AudioMaxMiB is the largest sound the form says it takes.
const AudioMaxMiB = audioinput.MaxMiB

func filesPath() string { return paths.GenerationTestChat("files") }
