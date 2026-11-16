package refform

import (
	"io"
	"mime/multipart"
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Read reads at most limit bytes of a file of the form (the caller passes the largest size it takes plus one, so that a file over it is seen as over it).
func Read(fh *multipart.FileHeader, limit int64) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, limit))
}

// pendingAudio is the id of the audio file that is chosen in the browser but not sent yet: the check of the inputs does not look at the file, only at its role.
const pendingAudio = "#audio"

// WithChosenAudio adds the audio file that the form (the input check, which posts urlencoded and so carries no file) has chosen as an input with the role audio:
// the browser names a chosen file by a non-empty upload_audio (htmx encodes a File as text), which is all the check needs to say "this model uses no audio"
// or "this model needs audio".
func WithChosenAudio(refs []gentest.RefInput, v url.Values) []gentest.RefInput {
	if v.Get(view.FieldAudio) == "" {
		return refs
	}
	return append(refs[:len(refs):len(refs)], gentest.RefInput{Source: gentest.FromStaged, ID: pendingAudio, Role: gentest.RoleAudio})
}

// BatchInputRow is the row of an input an earlier batch kept (an image, or the audio of a speech-to-video model), with the address of its stored file.
func BatchInputRow(r Value, role string) view.Ref {
	label := "アップロードした画像（前の生成の入力）"
	if role == gentest.RoleAudio {
		label = "アップロードした音声（前の生成の入力）"
	}
	return view.Ref{Value: r.String(), Label: label, ThumbURL: paths.GenerationTestPart("inputs", r.ID, "file")}
}

// StagedAudioRow is the row of an audio file that was staged for this form (no address: it is not served until a batch has taken it).
func StagedAudioRow(r Value) view.Ref {
	return view.Ref{Value: r.String(), Label: "アップロードした音声"}
}
