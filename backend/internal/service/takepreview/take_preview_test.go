//go:build unix

// Package takepreview_test tests the previews of Takes (TakeService.Video / Thumbnail) against real
// ffmpeg: a conversion that is cancelled, that fails, and that is still running at shutdown.
package takepreview_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/previewcache"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

type fixture struct {
	svc   *service.Services
	close func()
	logs  *bytes.Buffer
	file  string // the file of the Take "t1"

	mu      sync.Mutex
	writers []*os.File // the writing ends of the FIFO that goneOnceRunning holds
}

// newFixture is a Take "t1" (a 4K Take, so Video makes a proxy) whose file is a FIFO nobody writes
// to when hang is set: ffmpeg blocks opening it until it is killed, which stands for a conversion
// that takes minutes. Otherwise the file is not a video, so that ffmpeg fails at once.
func newFixture(t *testing.T, hang bool) *fixture {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	testenv.Parallel(t)
	f := &fixture{logs: new(bytes.Buffer)}
	dir := t.TempDir()
	f.file = filepath.Join(dir, "assets", "p1", "t1.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(f.file), 0o700))
	if hang {
		require.NoError(t, syscall.Mkfifo(f.file, 0o600))
	} else {
		require.NoError(t, os.WriteFile(f.file, []byte("not a video"), 0o600))
	}
	db := testdb.Open(t)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{}')`,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path) VALUES ('t1', 'pv1', 'kling-3.0', '{"resolution":"2160p"}', 'p1/t1.mp4')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	f.svc = service.New(service.Deps{
		DB: db, AssetsDir: filepath.Join(dir, "assets"), Media: &tools,
		Logger: slog.New(slog.NewTextHandler(f.logs, nil)),
	})
	f.close = sync.OnceFunc(f.svc.Close) // Services.Close is not for calling twice: a test that closes does it through f.close
	t.Cleanup(f.closeWriters)            // runs after f.close: the conversion is killed first, not given an end of file
	t.Cleanup(f.close)
	return f
}

// goneOnceRunning is a context of a request the browser cancels (a seek, a page change) once the
// conversion of the hanging file is running. It waits for the conversion, not for a time: how long a
// request takes to get to ffmpeg depends on the machine (-race is several times slower), and a
// request that is cancelled before it gets there starts no conversion at all.
//
// The conversion is running when ffmpeg has opened the FIFO: opening it for writing returns only
// then. The writer is held open, so ffmpeg goes on waiting for data until it is killed.
func (f *fixture) goneOnceRunning(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		w, err := os.OpenFile(f.file, os.O_WRONLY, 0)
		if err == nil {
			f.mu.Lock()
			f.writers = append(f.writers, w)
			f.mu.Unlock()
		}
		cancel()
	}()
	return ctx
}

func (f *fixture) closeWriters() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.writers {
		_ = w.Close()
	}
}

// A browser cancels what it asked for at every seek and card change. That is not a failure of the
// proxy: it is not logged as an error.
func TestVideo_AClientThatGivesUpIsNotLoggedAsAFailedProxy(t *testing.T) {
	f := newFixture(t, true)

	_, err := f.svc.Takes.Video(f.goneOnceRunning(t), "t1")

	require.NoError(t, err)
	assert.NotContains(t, f.logs.String(), "level=ERROR")
}

// The same for the thumbnail, all the way through the logging middleware: nothing is answered to a
// connection that is gone, and the request is not logged as a 500.
func TestThumbnail_AClientThatGivesUpIsNotLoggedAsAServerError(t *testing.T) {
	f := newFixture(t, true)
	engine := gin.New()
	engine.Use(middleware.Logger(slog.New(slog.NewTextHandler(f.logs, nil))))
	engine.GET("/takes/:id/thumbnail", handler.TakeThumbnail(f.svc.Takes))
	req := httptest.NewRequest(http.MethodGet, "/takes/t1/thumbnail", nil).WithContext(f.goneOnceRunning(t))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	assert.NotEqual(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, f.logs.String(), "level=ERROR")
}

// A thumbnail that failed is not made again for a while, as a proxy is not: a broken file would cost
// an ffmpeg run at every card.
func TestThumbnail_AFailureIsNotMadeAgainForAWhile(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	_, err := f.svc.Takes.Thumbnail(ctx, "t1")
	require.Error(t, err, "the first request tries and fails")
	require.NotErrorIs(t, err, service.ErrNotFound)

	_, err = f.svc.Takes.Thumbnail(ctx, "t1")

	assert.ErrorIs(t, err, service.ErrNotFound, "the failure is remembered: no ffmpeg run")
}

// Stopping the app kills the conversions that are running: they are not left to run as orphans.
func TestServicesClose_KillsTheRunningConversion(t *testing.T) {
	f := newFixture(t, true)
	_, err := f.svc.Takes.Video(f.goneOnceRunning(t), "t1") // ffmpeg is running, the request is gone
	require.NoError(t, err)

	closed := make(chan struct{})
	go func() { f.close(); close(closed) }()

	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Services.Close did not end the running conversion")
	}
	_, err = f.svc.Takes.Thumbnail(context.Background(), "t1")
	assert.ErrorIs(t, err, previewcache.ErrClosed, "no conversion starts after Close")
}
