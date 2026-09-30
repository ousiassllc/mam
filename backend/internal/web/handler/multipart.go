package handler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"

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

// readMultipart reads a multipart request of at most limit bytes. It is the only place that
// applies the upload limit and parses the form; see unreadable for the errors.
func readMultipart(c *gin.Context, limit int64) (*multipart.Form, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
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
