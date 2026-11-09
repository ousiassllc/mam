package screentest

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"

	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

// Upload sends an HTMX multipart POST with one file in the field (the "ファイルを追加" form of the model manager), as the browser's htmx does.
func (s *Screen) Upload(path, field, filename string, body []byte) *httptest.ResponseRecorder {
	s.T.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil {
		s.T.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		s.T.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		s.T.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}
