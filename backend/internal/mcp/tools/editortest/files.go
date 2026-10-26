package editortest

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
)

// Upload posts a file to the ticket of a purpose, as a client does with the URL create_upload_ticket gives.
func Upload(env *mcptest.Env, purpose string, params map[string]string, name string, data []byte) *httptest.ResponseRecorder {
	env.T.Helper()
	ticket, err := env.Registrar.Files.UploadTicketFor(context.Background(), purpose, params)
	require.NoError(env.T, err)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	require.NoError(env.T, err)
	_, err = part.Write(data)
	require.NoError(env.T, err)
	require.NoError(env.T, w.Close())
	req := httptest.NewRequest(http.MethodPost, ticket.URL, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	env.Registrar.Files.Handler().ServeHTTP(rec, req)
	return rec
}

// Download fetches the file behind a URL a tool returned.
func Download(env *mcptest.Env, url string) *httptest.ResponseRecorder {
	env.T.Helper()
	rec := httptest.NewRecorder()
	env.Registrar.Files.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

// Upload is the package function with the Fixture's Env.
func (f *Fixture) Upload(purpose string, params map[string]string, name string, data []byte) *httptest.ResponseRecorder {
	f.T.Helper()
	return Upload(f.Env, purpose, params, name, data)
}

// Download is the package function with the Fixture's Env.
func (f *Fixture) Download(url string) *httptest.ResponseRecorder {
	f.T.Helper()
	return Download(f.Env, url)
}
