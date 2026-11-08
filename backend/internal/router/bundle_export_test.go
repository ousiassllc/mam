package router_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/router"
)

// syncBuffer is a log sink the server goroutine writes while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// failingExport downloads the bundle of a Project one of whose files cannot be read (opening works, reading fails: the failure comes after the first
// bytes are written) through a server logging into logs. It returns the path requested and the error the client saw.
func failingExport(t *testing.T, logs *syncBuffer) (string, error) {
	src := newGenApp(t, true)
	src.generateAndWait()
	var video string
	require.NoError(t, src.db.Reader().QueryRow(`SELECT file_path FROM asset WHERE type = 'video' LIMIT 1`).Scan(&video))
	full := filepath.Join(src.assets, filepath.FromSlash(video))
	require.NoError(t, os.Remove(full))
	require.NoError(t, os.Mkdir(full, 0o750))
	engine := router.New(src.svc, router.Options{Logger: slog.New(slog.NewTextHandler(logs, nil))})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	path := "/projects/" + src.project + "/bundle"
	req, err := http.NewRequest(http.MethodGet, server.URL+path, http.NoBody)
	require.NoError(t, err)
	req.AddCookie(src.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		_, err = io.ReadAll(resp.Body)
	}
	return path, err
}

// A bundle that fails after the download started must not end like a finished download: the connection is cut, so the client sees a failed transfer.
func TestBundle_ExportThatFailsMidStreamIsNotDeliveredAsASuccess(t *testing.T) {
	_, err := failingExport(t, &syncBuffer{})
	assert.Error(t, err, "the client must see the export fail")
}

// The connection cut of a failed export leaves its trace: one error line with the request and the cause (the panic bypasses net/http's own logging).
func TestBundle_ExportThatFailsMidStreamIsLoggedWithItsCause(t *testing.T) {
	var logs syncBuffer
	path, err := failingExport(t, &logs)
	require.Error(t, err, "the client still sees the export fail")
	out := logs.String()
	assert.Equal(t, 1, strings.Count(out, "http request"), out)
	assert.Contains(t, out, "level=ERROR")
	assert.Contains(t, out, "method=GET")
	assert.Contains(t, out, "path="+path)
	assert.Contains(t, out, "is a directory", "the cause of the failed file")
}
