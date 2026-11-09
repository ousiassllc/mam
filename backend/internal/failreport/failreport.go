// Package failreport is what a service says of a failure of ffmpeg: the failure as the common
// error (#1010), the sentence the user reads (the cause and the remedy), and, for a failure nobody could put a cause to, an error ID that the
// sentence and the log line share, so that a person who reports the failure and the log meet.
//
// It is below the services (service wraps the result in its AIError) and above errmap, whose table it uses.
package failreport

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
)

// Media is a failed run of ffmpeg (the Rough Cut, the export, composing a Take, the audio mix) on the work that what names (the catalog key
// "what.export", ...): errmap.Media, and the sentence with the start of the detail (ffmpeg's own words are all there is to say beyond the
// remedy). A failure of ffmpeg that no cause is known for (media_failed) gets an error ID and is logged under it.
func Media(logger *slog.Logger, what string, err error) (*apperr.Error, string) {
	e := errmap.Media(what, err)
	settle(logger, e, "media processing failed")
	return e, reason(e, true)
}

// Line is err, a failure that no handler worded, as the one line a screen or a status text shows (#1010): errmap's cause and remedy, the start of the
// detail (the original message is never the line itself) and, for a failure nobody could put a cause to (unknown), an error ID that the log line
// msg shares. nil gives "".
func Line(logger *slog.Logger, msg string, err error) string {
	e := errmap.Explain(err)
	if e == nil {
		return ""
	}
	settle(logger, e, msg)
	return reason(e, true)
}

// settle gives a failure nobody could put a cause to (unknown, media_failed) an error ID and writes its cause to the log under it. The other
// failures are not logged here (their cause is their code). A caller that went away (a preview that was left) did not fail.
func settle(logger *slog.Logger, e *apperr.Error, msg string) {
	if errors.Is(e, context.Canceled) || (e.Code != apperr.CodeUnknown && e.Code != apperr.CodeMediaFailed) || e.ID != "" {
		return
	}
	e.ID = apperr.NewID()
	if logger != nil {
		logger.Error(msg, "error_id", e.ID, "code", string(e.Code), "err", e.Unwrap())
	}
}

// reason is the sentence a screen, a chat message and a tool error say: the cause and the remedy, with the start of the detail when withDetail
// is set, and the error ID of a failure that has one.
func reason(e *apperr.Error, withDetail bool) string {
	v := e.View(apperr.LangJa)
	s := v.Sentence(apperr.LangJa)
	if withDetail {
		s = v.Line(apperr.LangJa)
	}
	if e.ID != "" {
		s += "（" + apperr.Text(apperr.LangJa, "ui.error_id", nil) + ": " + e.ID + "）"
	}
	return s
}
