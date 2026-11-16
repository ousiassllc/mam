package chat

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// refusalOf is why the service refused an estimate, in the words the LLM and the thread are told: a refused input (*gterr.InvalidError), a conflict, or a request that was
// stopped as the common error (apperr: an input the model needs and the request lacks, a file no chosen model uses, a sound that cannot be used). hint is what the LLM is
// additionally told to do, in English ("" when the words say it). ok is false for any other error: a failure of the service, which is not the LLM's to fix.
func refusalOf(err error) (why, hint string, ok bool) {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
	)
	switch {
	case errors.As(err, &invalid):
		return invalid.Message, "", true
	case errors.As(err, &conflict):
		return conflict.Message, "", true
	}
	e, isApp := apperr.As(err)
	if !isApp || e.Class() != apperr.ClassValidation {
		return "", "", false
	}
	why = e.View(apperr.LangJa).Sentence(apperr.LangJa)
	if e.Code == apperr.CodeInputMissing {
		switch e.Args["param"] {
		case "audio":
			hint = "The model needs a sound. If the user has not attached one (no [attachment] audio line), ask them in Japanese to attach an audio file with the 音声ファイル button next to the message box; then estimate again with \"audio\": true."
		case "reference_image":
			hint = "The model needs a picture. If the user has not attached one (no [attachment] image line), ask them in Japanese to attach a picture with the 参照画像 button next to the message box; then estimate again with \"image\": true."
		default:
			hint = "The chat cannot give this input (only one sound and one reference picture can be attached here): tell the user to use the form of the 生成する tab, or choose another model."
		}
	}
	return why, hint, true
}
