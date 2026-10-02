package handler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// multipartMemory is how much of an upload Go keeps in memory before spilling to temp files.
const multipartMemory = 8 << 20

// requestError is a request body that cannot be read as sent: too large (413) or malformed (400).
// statusOf reports it with its own status and message.
type requestError struct {
	status  int
	message string
}

func (e *requestError) Error() string { return e.message }

// uploadIdleTimeout is how long an upload may stall between two reads. The server's ReadTimeout is
// an absolute deadline for the whole body, which an 8 GiB bundle can exceed on a slow link or disk
// even though it is within the limit; an upload is held to this idle time instead.
var uploadIdleTimeout = time.Minute

// idleDeadlineBody gives every read of an upload body a fresh read deadline of uploadIdleTimeout,
// replacing the absolute one of the server.
type idleDeadlineBody struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (b idleDeadlineBody) Read(p []byte) (int, error) {
	// Not every ResponseWriter supports deadlines; the server's own deadline then stays.
	_ = b.rc.SetReadDeadline(time.Now().Add(uploadIdleTimeout))
	return b.ReadCloser.Read(p)
}

// readMultipart reads a multipart request of at most limit bytes. It is the only place that
// applies the upload limit and parses the form; see unreadable for the errors. A request that
// declares more than limit is refused before a byte of it is read.
func readMultipart(c *gin.Context, limit int64) (*multipart.Form, error) {
	if c.Request.ContentLength > limit {
		return nil, &requestError{status: http.StatusRequestEntityTooLarge, message: "送信内容が大きすぎます"}
	}
	body := idleDeadlineBody{ReadCloser: c.Request.Body, rc: http.NewResponseController(c.Writer)}
	c.Request.Body = http.MaxBytesReader(c.Writer, body, limit)
	if err := c.Request.ParseMultipartForm(multipartMemory); err != nil {
		return nil, unreadable(c, err, "", "")
	}
	return c.Request.MultipartForm, nil
}

// unreadable is the error for a body that could not be read or bound: a 413 when it exceeded the
// size limit. Otherwise it is a ValidationError with msg on field when msg is given, else a 400
// whose cause is logged.
func unreadable(c *gin.Context, err error, field, msg string) error {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return &requestError{status: http.StatusRequestEntityTooLarge, message: "送信内容が大きすぎます"}
	case msg != "":
		return &service.ValidationError{Field: field, Message: msg}
	}
	_ = c.Error(err)
	return &requestError{status: http.StatusBadRequest, message: "送信内容の形式が正しくありません"}
}

// namedFile is the first file of the field, or nil: an empty file input is sent as an unnamed
// empty part, which counts as no file.
func namedFile(form *multipart.Form, field string) *multipart.FileHeader {
	if files := form.File[field]; len(files) > 0 && files[0].Filename != "" {
		return files[0]
	}
	return nil
}

// firstFile is the required file of the field; without one it is a ValidationError with missingMsg.
func firstFile(form *multipart.Form, field, missingMsg string) (*multipart.FileHeader, error) {
	if fh := namedFile(form, field); fh != nil {
		return fh, nil
	}
	return nil, &service.ValidationError{Field: field, Message: missingMsg}
}

// optionalUpload is the file of the field as an Upload; the zero Upload when none was sent
// (the service then reports what is missing).
func optionalUpload(form *multipart.Form, field string) service.Upload {
	if fh := namedFile(form, field); fh != nil {
		return uploadOf(fh)
	}
	return service.Upload{}
}

func uploadOf(fh *multipart.FileHeader) service.Upload {
	return service.Upload{
		Name: fh.Filename,
		Size: fh.Size,
		Open: func() (io.ReadCloser, error) { return fh.Open() },
	}
}
