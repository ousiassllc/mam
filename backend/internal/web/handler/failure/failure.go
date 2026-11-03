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
