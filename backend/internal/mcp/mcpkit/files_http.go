package mcpkit

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service"
)

const (
	multipartMemory = 8 << 20
	// uploadSlack is room in the request for the form fields and boundaries around the file.
	uploadSlack = 1 << 20
	// uploadIdleTimeout is how long an upload may stall between two reads (the listener has no overall read deadline).
	uploadIdleTimeout = time.Minute
)

// Handler serves FilesPath + ticket: GET / HEAD download the file of a download ticket, POST of a
// multipart form uploads the file of an upload ticket (field FileField). The ticket in the URL is the
// credential; the server in front of it (mcp/server) additionally refuses a bearer token that is
// present but not valid. Anything wrong with the ticket (unknown, expired, used, of the other direction)
// is the same 404, so that a ticket cannot be told from a guess.
func (f *Files) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := strings.TrimPrefix(r.URL.Path, FilesPath)
		if value == "" || strings.Contains(value, "/") {
			writeError(w, notFound())
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			f.serveDownload(w, r, value)
		case http.MethodPost:
			f.serveUpload(w, r, value)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			writeError(w, &ToolError{Code: CodeValidation, Message: "GET か POST で呼んでください", Status: http.StatusMethodNotAllowed})
		}
	})
}

func notFound() *ToolError {
	return &ToolError{Code: CodeNotFound, Message: "URL が無効か、期限が切れています", Status: http.StatusNotFound}
}

func writeError(w http.ResponseWriter, e *ToolError) {
	body := map[string]string{"code": e.Code, "message": e.Message}
	if e.ErrorID != "" {
		body[ErrorIDKey] = e.ErrorID
	}
	writeJSON(w, e.Status, map[string]any{"error": body})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// live returns the ticket of the right direction whose issuing token still exists.
func (f *Files) live(r *http.Request, value string, dir direction) (*ticket, *ToolError) {
	t := f.lookup(value, dir)
	if t == nil {
		return nil, notFound()
	}
	if t.tokenID != "" && f.authorize != nil && !f.authorize(r.Context(), t.tokenID) {
		return nil, &ToolError{Code: CodeValidation, Message: "このチケットを発行したトークンは失効しています", Status: http.StatusUnauthorized}
	}
	return t, nil
}

func (f *Files) serveDownload(w http.ResponseWriter, r *http.Request, value string) {
	t, terr := f.live(r, value, download)
	if terr != nil {
		writeError(w, terr)
		return
	}
	f.mu.Lock()
	fn := f.downloads[t.kind]
	f.mu.Unlock()
	file, err := fn(r.Context(), t.id)
	if err != nil {
		writeError(w, ReportError(f.logger, "mcp download failed", err, "kind", t.kind))
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.Name})
	w.Header().Set("Content-Disposition", disposition)
	if file.Write != nil {
		ct := file.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		if r.Method == http.MethodHead {
			return
		}
		_ = file.Write(w) // the status is sent already; a broken stream shows as a short body
		return
	}
	body, err := os.Open(file.Path) // #nosec G304 -- the path comes from a DownloadFunc of the service layer, not from the request
	if err != nil {
		writeError(w, MapError(service.ErrNotFound))
		return
	}
	defer func() { _ = body.Close() }()
	info, err := body.Stat()
	if err != nil || info.IsDir() {
		writeError(w, MapError(service.ErrNotFound))
		return
	}
	if file.ContentType != "" {
		w.Header().Set("Content-Type", file.ContentType)
	} else if mt := mime.TypeByExtension(filepath.Ext(file.Name)); mt != "" {
		w.Header().Set("Content-Type", mt)
	}
	http.ServeContent(w, r, file.Name, info.ModTime(), body)
}

func (f *Files) serveUpload(w http.ResponseWriter, r *http.Request, value string) {
	t, terr := f.live(r, value, upload)
	if terr != nil {
		writeError(w, terr)
		return
	}
	f.mu.Lock()
	spec := f.uploads[t.kind]
	f.mu.Unlock()

	limit := spec.MaxBytes + uploadSlack
	if r.ContentLength > limit {
		writeError(w, tooLarge())
		return
	}
	if !f.consume(value) {
		writeError(w, notFound())
		return
	}
	r.Body = http.MaxBytesReader(w, idleBody{ReadCloser: r.Body, rc: http.NewResponseController(w)}, limit)
	if err := r.ParseMultipartForm(multipartMemory); err != nil { // #nosec G120 -- the body is limited by http.MaxBytesReader above
		f.release(value)
		var big *http.MaxBytesError
		if errors.As(err, &big) {
			writeError(w, tooLarge())
			return
		}
		writeError(w, MapError(&service.ValidationError{Field: FileField, Message: "multipart/form-data で送ってください"}))
		return
	}
	headers := r.MultipartForm.File[FileField]
	if len(headers) == 0 || headers[0].Filename == "" {
		f.release(value)
		writeError(w, MapError(&service.ValidationError{Field: FileField, Message: "ファイルを送ってください"}))
		return
	}
	result, err := spec.Handle(r.Context(), UploadInput{Params: t.params, Header: headers[0]})
	if err != nil {
		f.release(value) // nothing was stored: the same ticket can carry a corrected file
		writeError(w, ReportError(f.logger, "mcp upload failed", err, "purpose", spec.Purpose))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"purpose": spec.Purpose, "result": result})
}

func tooLarge() *ToolError {
	return &ToolError{Code: CodeValidation, Message: "送信内容が大きすぎます", Status: http.StatusRequestEntityTooLarge}
}

// idleBody gives every read of an upload a fresh read deadline.
type idleBody struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (b idleBody) Read(p []byte) (int, error) {
	_ = b.rc.SetReadDeadline(time.Now().Add(uploadIdleTimeout)) // not every writer supports deadlines
	return b.ReadCloser.Read(p)
}
