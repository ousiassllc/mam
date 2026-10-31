package mcpkit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
)

type fileEnv struct {
	f     *Files
	clock *time.Time
	dir   string
}

func newFileEnv(t *testing.T) *fileEnv {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	e := &fileEnv{f: NewFiles(FilesOptions{}), clock: &now, dir: t.TempDir()}
	e.f.now = func() time.Time { return *e.clock }
	require.NoError(t, os.WriteFile(filepath.Join(e.dir, "a.mp4"), []byte("0123456789"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(e.dir, "b.mp4"), []byte("bbbb"), 0o600))
	e.f.RegisterDownload("clip", func(_ context.Context, id string) (DownloadFile, error) {
		switch id {
		case "a", "b":
			return DownloadFile{Name: id + ".mp4", ContentType: "video/mp4", Path: filepath.Join(e.dir, id+".mp4")}, nil
		case "gen":
			return DownloadFile{Name: "gen.zip", Write: func(w io.Writer) error { _, err := io.WriteString(w, "zipbytes"); return err }}, nil
		}
		return DownloadFile{}, service.ErrNotFound
	})
	return e
}

func (e *fileEnv) do(method, url string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	e.f.Handler().ServeHTTP(rec, req)
	return rec
}

func pathOf(link Link) string { return link.URL[strings.Index(link.URL, FilesPath):] }

func TestDownloadURL_IsBoundToOneResourceAndExpires(t *testing.T) {
	e := newFileEnv(t)
	ctx := context.Background()
	a, err := e.f.DownloadURL(ctx, "clip", "a")
	require.NoError(t, err)
	b, err := e.f.DownloadURL(ctx, "clip", "b")
	require.NoError(t, err)
	assert.NotEqual(t, a.URL, b.URL)
	assert.Equal(t, "a.mp4", a.FileName)
	assert.True(t, a.ExpiresAt.Equal(e.clock.Add(TicketTTL)))

	rec := e.do(http.MethodGet, pathOf(a), nil, "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "0123456789", rec.Body.String(), "a's ticket gives a's file, not b's")
	assert.Equal(t, "video/mp4", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `filename=a.mp4`)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "bbbb", e.do(http.MethodGet, pathOf(b), nil, "").Body.String())

	// A ticket works again (a client retries, asks for a range) until it expires.
	req := httptest.NewRequest(http.MethodGet, pathOf(a), nil)
	req.Header.Set("Range", "bytes=2-4")
	partial := httptest.NewRecorder()
	e.f.Handler().ServeHTTP(partial, req)
	assert.Equal(t, http.StatusPartialContent, partial.Code)
	assert.Equal(t, "234", partial.Body.String())

	*e.clock = e.clock.Add(TicketTTL + time.Second)
	rec = e.do(http.MethodGet, pathOf(a), nil, "")
	assert.Equal(t, http.StatusNotFound, rec.Code, "expired")
	assert.NotContains(t, rec.Body.String(), "0123456789")
}

func TestDownloadURL_StreamedFilesAndMissingResources(t *testing.T) {
	e := newFileEnv(t)
	link, err := e.f.DownloadURL(context.Background(), "clip", "gen")
	require.NoError(t, err)
	rec := e.do(http.MethodGet, pathOf(link), nil, "")
	assert.Equal(t, "zipbytes", rec.Body.String())
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))

	_, err = e.f.DownloadURL(context.Background(), "clip", "missing")
	assert.ErrorIs(t, err, service.ErrNotFound, "a resource with no file never gets a URL")
	_, err = e.f.DownloadURL(context.Background(), "nokind", "a")
	assert.Error(t, err)
}

// alteredLastChar returns s with its last character replaced by a different one, always.
// (Replacing it with a fixed character leaves s unchanged when s already ends in that character.)
func alteredLastChar(s string) string {
	last := "A"
	if strings.HasSuffix(s, last) {
		last = "B"
	}
	return s[:len(s)-1] + last
}

func TestAlteredLastChar_AlwaysDiffers(t *testing.T) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	for _, c := range alphabet {
		s := "xyz" + string(c)
		got := alteredLastChar(s)
		assert.NotEqual(t, s, got, "last char %q", c)
		assert.Len(t, got, len(s))
		assert.Equal(t, s[:len(s)-1], got[:len(got)-1])
	}
}

func TestHandler_AnythingWrongWithATicketIsTheSame404(t *testing.T) {
	e := newFileEnv(t)
	link, err := e.f.DownloadURL(context.Background(), "clip", "a")
	require.NoError(t, err)
	ticket := strings.TrimPrefix(pathOf(link), FilesPath)

	for name, path := range map[string]string{
		"no ticket":     FilesPath,
		"unknown":       FilesPath + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"with a suffix": FilesPath + ticket + "/x",
		"altered":       FilesPath + alteredLastChar(ticket),
	} {
		rec := e.do(http.MethodGet, path, nil, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, name)
		assert.JSONEq(t, `{"error":{"code":"not_found","message":"URL が無効か、期限が切れています"}}`, rec.Body.String(), name)
	}
	rec := e.do(http.MethodPost, FilesPath+ticket, strings.NewReader(""), "multipart/form-data; boundary=x")
	assert.Equal(t, http.StatusNotFound, rec.Code, "a download ticket cannot upload")
	rec = e.do(http.MethodDelete, FilesPath+ticket, nil, "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func multipartBody(t *testing.T, field, name string, data []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if field != "" {
		part, err := w.CreateFormFile(field, name)
		require.NoError(t, err)
		_, err = part.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

func registerNotes(e *fileEnv, handle func(UploadInput) (any, error)) {
	e.f.RegisterUpload(UploadSpec{
		Purpose: "note", Description: "d", MaxBytes: 16,
		Params: []UploadParam{{Name: "project_id", Required: true}, {Name: "tag"}},
		Handle: func(_ context.Context, in UploadInput) (any, error) { return handle(in) },
	})
}

func TestUploadTicket_IsSingleUseBoundToItsParamsAndRetriableAfterARefusal(t *testing.T) {
	e := newFileEnv(t)
	var got []map[string]string
	var fail error
	registerNotes(e, func(in UploadInput) (any, error) {
		if fail != nil {
			return nil, fail
		}
		f, err := in.Header.Open()
		require.NoError(t, err)
		data, _ := io.ReadAll(f)
		got = append(got, map[string]string{"project": in.Params["project_id"], "tag": in.Params["tag"], "body": string(data), "name": in.Upload().Name})
		return map[string]string{"id": "n1"}, nil
	})
	ticket, err := e.f.UploadTicketFor(context.Background(), "note", map[string]string{"project_id": "p1", "tag": "t"})
	require.NoError(t, err)
	assert.Equal(t, "POST", ticket.Method)
	assert.Equal(t, FileField, ticket.FileField)
	assert.EqualValues(t, 16, ticket.MaxBytes)
	assert.Contains(t, ticket.Curl, ticket.URL)
	path := pathOf(ticket.Link)

	// A refused upload (the service said no) does not burn the ticket.
	fail = &service.ValidationError{Field: "file", Message: "対応していないファイル形式です"}
	body, ct := multipartBody(t, FileField, "x.txt", []byte("bad"))
	rec := e.do(http.MethodPost, path, body, ct)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "対応していないファイル形式です")

	// A request with no file does not burn it either.
	body, ct = multipartBody(t, "", "", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(http.MethodPost, path, body, ct).Code)

	fail = nil
	body, ct = multipartBody(t, FileField, "../../evil.txt", []byte("hello"))
	rec = e.do(http.MethodPost, path, body, ct)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"purpose":"note","result":{"id":"n1"}}`, rec.Body.String())
	require.Len(t, got, 1)
	assert.Equal(t, "p1", got[0]["project"], "the project comes from the ticket, not from the request")
	assert.Equal(t, "hello", got[0]["body"])

	body, ct = multipartBody(t, FileField, "x.txt", []byte("again"))
	assert.Equal(t, http.StatusNotFound, e.do(http.MethodPost, path, body, ct).Code, "used up")
	assert.Len(t, got, 1)
}

func TestUploadTicket_LimitsAndParameterChecks(t *testing.T) {
	e := newFileEnv(t)
	registerNotes(e, func(UploadInput) (any, error) { return "ok", nil })
	ctx := context.Background()

	for name, tc := range map[string]struct {
		purpose string
		params  map[string]string
		field   string
	}{
		"unknown purpose":  {"nope", nil, "purpose"},
		"missing required": {"note", map[string]string{"tag": "x"}, "project_id"},
		"blank required":   {"note", map[string]string{"project_id": "  "}, "project_id"},
		"undeclared param": {"note", map[string]string{"project_id": "p", "other": "x"}, "other"},
	} {
		_, err := e.f.UploadTicketFor(ctx, tc.purpose, tc.params)
		var invalid *service.ValidationError
		require.ErrorAs(t, err, &invalid, name)
		assert.Equal(t, tc.field, invalid.Field, name)
	}

	ticket, err := e.f.UploadTicketFor(ctx, "note", map[string]string{"project_id": "p"})
	require.NoError(t, err)
	body, ct := multipartBody(t, FileField, "big.bin", bytes.Repeat([]byte("x"), 16+uploadSlack+1))
	rec := e.do(http.MethodPost, pathOf(ticket.Link), body, ct)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

	// Too large is refused before storing, so the ticket is still good for a file within the limit.
	body, ct = multipartBody(t, FileField, "ok.bin", []byte("small"))
	assert.Equal(t, http.StatusCreated, e.do(http.MethodPost, pathOf(ticket.Link), body, ct).Code)

	*e.clock = e.clock.Add(TicketTTL + time.Second)
	fresh, err := e.f.UploadTicketFor(ctx, "note", map[string]string{"project_id": "p"})
	require.NoError(t, err)
	*e.clock = e.clock.Add(TicketTTL + time.Second)
	body, ct = multipartBody(t, FileField, "ok.bin", []byte("small"))
	assert.Equal(t, http.StatusNotFound, e.do(http.MethodPost, pathOf(fresh.Link), body, ct).Code, "expired")
}

func TestTickets_DieWithTheTokenThatIssuedThem(t *testing.T) {
	e := newFileEnv(t)
	active := true
	e.f.authorize = func(_ context.Context, id string) bool { return id == "tok" && active }
	ctx := withToken(t, "tok")
	link, err := e.f.DownloadURL(ctx, "clip", "a")
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, e.do(http.MethodGet, pathOf(link), nil, "").Code)
	active = false
	rec := e.do(http.MethodGet, pathOf(link), nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "0123456789")
}

func TestDownloadURL_UsesTheHostTheClientReachedTheServerWith(t *testing.T) {
	e := newFileEnv(t)
	e.f.fallbackBase = func() string { return "http://127.0.0.1:8788" }

	viaTailnet, err := e.f.DownloadURL(WithBaseURL(context.Background(), "http://100.101.102.103:8788"), "clip", "a")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(viaTailnet.URL, "http://100.101.102.103:8788/mcp-files/"), viaTailnet.URL)
	inMemory, err := e.f.DownloadURL(context.Background(), "clip", "a")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(inMemory.URL, "http://127.0.0.1:8788/mcp-files/"), inMemory.URL)
}

func TestRegister_DuplicatesAreProgrammingErrors(t *testing.T) {
	e := newFileEnv(t)
	assert.Panics(t, func() { e.f.RegisterDownload("clip", nil) })
	registerNotes(e, func(UploadInput) (any, error) { return nil, errors.New("x") })
	assert.Panics(t, func() { registerNotes(e, nil) })
	assert.Panics(t, func() { e.f.RegisterUpload(UploadSpec{Purpose: "p"}) }, "a purpose needs a handler and a limit")
}
