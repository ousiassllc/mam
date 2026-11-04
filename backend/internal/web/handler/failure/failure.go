// Package failure is the answer of a screen to an error that no handler worded (#1010): the cause and the remedy in Japanese and the status
// of the class when it is a failure the app knows (an outside service, the disk, a model: package errmap, or an *apperr.Error), and the
// generic 500 for any other. It is a package of its own so that package handler stays within its size.
package failure

import (
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
)

// Generic is what the screen says of a failure nobody knows: its cause is in the log, not on the page.
const Generic = "内部エラーが発生しました"

// Fallback is the status and the message of err.
func Fallback(err error) (status int, msg string) {
	if e, ok := errmap.Known(err); ok && e.Code != apperr.CodeUnknown {
		return e.Status(), e.View(apperr.LangJa).Sentence(apperr.LangJa)
	}
	return http.StatusInternalServerError, Generic
}

// Save is the status and the message of a setting that could not be written (#1010): the cause and the remedy of what errmap knows of the
// disk (a full disk, a folder that cannot be written), else of the code save_failed. It is the same failure as the settings tools of the MCP
// answer with (errmap.SaveFailure), so that a screen and an agent read one text. The cause of the failure is not on the screen: the caller
// attaches it to the context, where the logging middleware writes it.
func Save(err error) (status int, msg string) {
	e := errmap.SaveFailure(err)
	return e.Status(), e.View(apperr.LangJa).Sentence(apperr.LangJa)
}
