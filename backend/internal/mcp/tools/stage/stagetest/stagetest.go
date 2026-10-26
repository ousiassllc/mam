// Package stagetest uploads a file through a ticket the way an MCP client does, for the tests of the tools
// that take staged uploads.
package stagetest

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
)

// Upload issues a ticket for purpose with params, POSTs the file to it and returns the status and the
// "result" of the answer (nil when the upload failed).
func Upload(env *mcptest.Env, purpose string, params map[string]string, name string, data []byte) (int, map[string]any) {
	env.T.Helper()
	ticket, err := env.Registrar.Files.UploadTicketFor(env.T.Context(), purpose, params)
	require.NoError(env.T, err)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile(mcpkit.FileField, name)
	require.NoError(env.T, err)
	_, err = part.Write(data)
	require.NoError(env.T, err)
	require.NoError(env.T, w.Close())

	req := httptest.NewRequest(http.MethodPost, ticket.URL[strings.Index(ticket.URL, mcpkit.FilesPath):], &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	env.Registrar.Files.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		return rec.Code, nil
	}
	var out struct {
		Result map[string]any `json:"result"`
	}
	require.NoError(env.T, json.Unmarshal(rec.Body.Bytes(), &out))
	return rec.Code, out.Result
}

// PNG and MP4 are the smallest bytes service/upload.go accepts as an image and a video.
var (
	PNG = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	MP4 = append([]byte("\x00\x00\x00\x18ftypisom"), make([]byte, 32)...)
)
