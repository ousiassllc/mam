//go:build unix

package router_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// hang replaces the file of the Take with a FIFO nobody writes to: ffmpeg / ffprobe block opening
// it until they are killed, which stands for a conversion that takes minutes.
func (ta *takeApp) hang(name string) {
	ta.t.Helper()
	path := filepath.Join(ta.dir, "assets", ta.project, name+".mp4")
	require.NoError(ta.t, os.Remove(path))
	require.NoError(ta.t, syscall.Mkfifo(path, 0o600))
}

// newPreviewApp is a takeApp whose service and router log into the returned buffer.
func newPreviewApp(t *testing.T) (*takeApp, *bytes.Buffer) {
	t.Helper()
	logs := new(bytes.Buffer)
	logger := slog.New(slog.NewTextHandler(logs, nil))
	ta := newTakeAppWith(t, func(d *service.Deps) { d.Logger = logger })
	ta.engine = router.New(ta.svc, router.Options{Logger: logger})
	return ta, logs
}

// A browser cancels what it asked for at every seek and every card change. That is not a failure:
// nothing is logged as an error.
func TestTakePreview_AClientThatGivesUpIsNotAnError(t *testing.T) {
	ta, logs := newPreviewApp(t)
	ta.hang("t1") // thumbnail of t1
	ta.hang("t3") // proxy of the 4K Take t3

	for _, path := range []string{"/takes/t1/thumbnail", "/takes/t3/video"} {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		req.AddCookie(ta.cookie)
		do(ta.engine, req)
		cancel()
	}

	assert.NotContains(t, logs.String(), "level=ERROR")
}

// A thumbnail that failed is not made again for a while, as a proxy is not: a broken file would cost
// an ffmpeg run at every card.
func TestTakePreview_AFailedThumbnailIsNotMadeAgainForAWhile(t *testing.T) {
	ta, _ := newPreviewApp(t)
	path := filepath.Join(ta.dir, "assets", ta.project, "t1.mp4")
	require.NoError(t, os.WriteFile(path, []byte("not a video"), 0o600))
	assert.Equal(t, http.StatusInternalServerError, ta.get("/takes/t1/thumbnail", false).Code, "the first request tries and fails")

	ta.video("t1", "1920x1080", 4) // the file is fine now, but the failure is remembered

	assert.Equal(t, http.StatusNotFound, ta.get("/takes/t1/thumbnail", false).Code)
}

// Stopping the app kills the conversions that are running: they are not left to run as orphans.
func TestServicesClose_KillsTheRunningPreviewConversions(t *testing.T) {
	ta, _ := newPreviewApp(t)
	ta.hang("t3")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel) // the proxy conversion is running, the request is gone
	req := httptest.NewRequest(http.MethodGet, "/takes/t3/video", nil).WithContext(ctx)
	req.AddCookie(ta.cookie)
	do(ta.engine, req)

	closed := make(chan struct{})
	go func() { ta.svc.Close(); close(closed) }()

	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Services.Close did not end the running conversion")
	}
}
