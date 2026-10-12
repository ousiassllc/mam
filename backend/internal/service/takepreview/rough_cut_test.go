//go:build unix

package takepreview_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/previewcache"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

type roughFixture struct {
	svc    *service.Services
	close  func()
	take   string // the file of the Take "t1"
	rough  string // where the Rough Cut of the Edit Timeline version "v1" is made
	assets string
}

// newRoughFixture is an Edit Timeline version "v1" of one clip: the Take "t1", whose file is a
// 2 s video when valid is set and is not a video otherwise.
func newRoughFixture(t *testing.T, valid bool) *roughFixture {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	testenv.Parallel(t)
	dir := t.TempDir()
	f := &roughFixture{assets: filepath.Join(dir, "assets")}
	f.take = filepath.Join(f.assets, "p1", "t1.mp4")
	f.rough = filepath.Join(f.assets, ".previews", "edit-v1.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(f.take), 0o700))
	if valid {
		f.writeValidTake(t)
	} else {
		require.NoError(t, os.WriteFile(f.take, []byte("not a video"), 0o600))
	}
	db := testdb.Open(t)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 2, 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{}')`,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path) VALUES ('t1', 'pv1', 'kling-3.0', '{"resolution":"1080p"}', 'p1/t1.mp4')`,
			`INSERT INTO edit_timeline_version (id, project_id, version_number, timeline) VALUES ('v1', 'p1', 1, '{"clips":[{"shot_id":"sh1","take_id":"t1","in_point":0,"out_point":1}]}')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	f.svc = service.New(service.Deps{DB: db, AssetsDir: f.assets, Media: &tools, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	f.close = sync.OnceFunc(f.svc.Close) // Services.Close is not for calling twice
	t.Cleanup(f.close)
	return f
}

func (f *roughFixture) writeValidTake(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile(f.take, testenv.PatternVideo(t, "320x240", 2), 0o600))
}

// A browser asks for the preview again at every seek. What is there is served as it is: the
// preview is not looked into (ffprobe) nor made again.
func TestRoughCutPreview_AFileThatIsThereIsServedWithoutBeingProbedOrRendered(t *testing.T) {
	f := newRoughFixture(t, true)
	require.NoError(t, os.MkdirAll(filepath.Dir(f.rough), 0o700))
	require.NoError(t, os.WriteFile(f.rough, []byte("rendered before"), 0o600)) // not a video: a probe would reject it and render again

	for range 3 {
		file, err := f.svc.Editor.Preview(context.Background(), "v1")
		require.NoError(t, err)
		assert.Equal(t, f.rough, file.Path)
	}

	got, err := os.ReadFile(f.rough)
	require.NoError(t, err)
	assert.Equal(t, "rendered before", string(got))
}

// A Rough Cut that failed is not rendered again for a while: a Take that cannot be read would cost
// the whole render (up to the time limit) at every Range request.
func TestRoughCutPreview_AFailureIsNotRenderedAgainForAWhile(t *testing.T) {
	f := newRoughFixture(t, false)
	ctx := context.Background()
	_, err := f.svc.Editor.Preview(ctx, "v1")
	require.Error(t, err, "the first request tries and fails")
	require.NotErrorIs(t, err, previewcache.ErrFailedRecently)
	f.writeValidTake(t) // what would make a second try succeed

	_, err = f.svc.Editor.Preview(ctx, "v1")

	assert.ErrorIs(t, err, previewcache.ErrFailedRecently, "the failure is remembered: nothing is rendered")
	assert.NoFileExists(t, f.rough)
}

// A seek or a card change cancels the request, not the render: the next request finds the file.
//
// The client gives up once the render is running (ffmpeg is writing its temporary file), not after
// a fixed time: a request that is cancelled before it reaches Make starts no render at all, and
// how long it takes to get there depends on the machine (-race is several times slower).
func TestRoughCutPreview_ARenderIsNotStoppedByAClientThatGivesUp(t *testing.T) {
	f := newRoughFixture(t, true)
	renderStarted := filepath.Join(filepath.Dir(f.rough), "."+filepath.Base(f.rough)+".ffmpeg.tmp")
	ctx, giveUp := context.WithCancel(context.Background())
	t.Cleanup(giveUp)
	asked := make(chan error, 1)
	go func() {
		_, err := f.svc.Editor.Preview(ctx, "v1")
		asked <- err
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(renderStarted)
		return err == nil
	}, 30*time.Second, time.Millisecond, "the render starts")
	giveUp()

	select {
	case err := <-asked:
		require.ErrorIs(t, err, context.Canceled, "nobody waits for the render")
	case <-time.After(30 * time.Second):
		require.FailNow(t, "the request is still waiting for the render")
	}
	require.Eventually(t, func() bool {
		_, err := os.Stat(f.rough)
		return err == nil
	}, 30*time.Second, 20*time.Millisecond, "the render goes on without the client")
	file, err := f.svc.Editor.Preview(context.Background(), "v1")
	require.NoError(t, err)
	assert.Equal(t, f.rough, file.Path)
}

// A request that is gone before it reaches Make starts no render.
func TestRoughCutPreview_ARequestThatIsGoneBeforeTheRenderStartsMakesNothing(t *testing.T) {
	f := newRoughFixture(t, true)
	ctx, giveUp := context.WithCancel(context.Background())
	giveUp()

	_, err := f.svc.Editor.Preview(ctx, "v1")

	require.Error(t, err)
	f.close() // waits for the renders that are running: none
	assert.NoFileExists(t, f.rough)
}

// The Rough Cut is made by the cache Services.Close stops (the one of the Take previews): after it
// no render starts.
func TestRoughCutPreview_ServicesCloseRefusesANewRender(t *testing.T) {
	f := newRoughFixture(t, true)
	f.close()

	_, err := f.svc.Editor.Preview(context.Background(), "v1")

	assert.ErrorIs(t, err, previewcache.ErrClosed)
}
