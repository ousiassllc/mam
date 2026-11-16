package chatscreen

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"

	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
)

// formSlack is what a multipart body may hold beyond the file (the fields and the boundaries); formMemory is how much of it the parser keeps in memory.
const (
	formSlack  = 1 << 20
	formMemory = 8 << 20
)

// Attach answers POST /generation-test/chat/files (multipart: session, kind audio|image, file): the file the user attached to the conversation (Issue #1153). The
// kind of the bytes is read by the service (gentest.Service.StageAudio / StageReference: the form's words), not taken from the name or the type the browser sent. The
// answer is a line added to the thread, or the reason (422 for a file that cannot be used, 413 for a body over the largest file). Nothing is generated.
func (h Handlers) Attach(c *gin.Context) {
	// The body is bounded before it is read: the largest file the chat takes (a sound) and the fields.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, audiofile.MaxBytes+formSlack)
	err := c.Request.ParseMultipartForm(formMemory)
	session := c.Request.PostFormValue(view.FieldSession)
	var big *http.MaxBytesError
	switch {
	case errors.As(err, &big) || (err != nil && strings.Contains(err.Error(), "request body too large")): // the multipart reader may hide the cause behind its own error
		refuseWith(c, session, http.StatusRequestEntityTooLarge, "ファイルが大きすぎます（音声ファイルは "+strconv.Itoa(view.AudioMaxMiB)+" MiB、画像は "+strconv.Itoa(view.MaxImageMiB)+" MiB まで）")
		return
	case err != nil:
		refuseWith(c, session, http.StatusUnprocessableEntity, "ファイルを読めませんでした。もう一度選んでください")
		return
	}
	kind, limit := attachKind(c.Request.PostFormValue(view.FieldKind))
	if limit == 0 {
		refuseWith(c, session, http.StatusUnprocessableEntity, "添付できるのは音声ファイルか参照画像です")
		return
	}
	fh, err := c.FormFile(view.FieldFile)
	if err != nil {
		refuseWith(c, session, http.StatusUnprocessableEntity, "ファイルを選んでください")
		return
	}
	data, err := refform.Read(fh, limit+1) // one byte more than the limit: a larger file is refused by the service, not cut to the limit and accepted
	if err != nil {
		refuse(c, session, err)
		return
	}
	rep, err := h.Chat.Attach(c.Request.Context(), session, kind, fh.Filename, data)
	if err != nil {
		refuse(c, session, err)
		return
	}
	h.render(c, http.StatusOK, session, rep)
}

// attachKind is the kind a form value names and the largest size of its file; the limit is 0 for any other value.
func attachKind(v string) (kind string, limit int64) {
	switch v {
	case view.KindAudioFile:
		return chat.AttachAudio, audiofile.MaxBytes
	case view.KindImageFile:
		return chat.AttachImage, gentest.MaxImageBytes
	}
	return "", 0
}
