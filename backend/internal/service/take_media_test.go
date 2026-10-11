package service_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// fakeMedia is a Take whose model reported no resolution, played through ffmpeg / ffprobe that are
// scripts: ffprobe says the picture is height pixels tall, ffmpeg runs ffmpegBody. Every run of
// either one is counted in the logs.
type fakeMedia struct {
	f       genFixture
	takeID  string
	src     string
	probes  string
	ffmpegs string
}

func newFakeMedia(t *testing.T, height, ffmpegBody string) fakeMedia {
	t.Helper()
	bin := t.TempDir()
	m := fakeMedia{takeID: "t1", probes: filepath.Join(bin, "ffprobe.log"), ffmpegs: filepath.Join(bin, "ffmpeg.log")}
	scripts := map[string]string{
		"ffprobe": "echo x >> " + m.probes + "\necho '{\"format\":{\"duration\":\"1\"},\"streams\":[{\"codec_type\":\"video\",\"width\":3840,\"height\":" + height + ",\"r_frame_rate\":\"24\"}]}'\n",
		"ffmpeg":  "echo x >> " + m.ffmpegs + "\n" + ffmpegBody,
	}
	for name, body := range scripts {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o700)) // #nosec G306 -- a test executable
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tools, err := media.Look()
	require.NoError(t, err)
	m.f = newGenFixtureWithDeps(t, false, func(string) (uint64, error) { return 1 << 40, nil }, func(d *service.Deps) { d.Media = &tools })
	require.NoError(t, os.MkdirAll(filepath.Join(m.f.assetsDir, "p"), 0o750))
	m.src = filepath.Join(m.f.assetsDir, "p", "t1.mp4")
	require.NoError(t, os.WriteFile(m.src, []byte("x"), 0o600))
	require.NoError(t, m.f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', ?, 1, '{}')`, m.f.shotID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path) VALUES ('t1', 'pv1', 'kling-3.0', '{}', 'p/t1.mp4')`)
		return err
	}))
	return m
}

func runs(t *testing.T, log string) int {
	t.Helper()
	b, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(b), "x")
}

func TestTakeVideo_AProxyThatFailedIsNotTriedAgainByTheNextRangeRequest(t *testing.T) {
	m := newFakeMedia(t, "2160", "exit 1\n")

	for range 3 {
		got, err := m.f.svc.Takes.Video(context.Background(), m.takeID)
		require.NoError(t, err)
		assert.Equal(t, m.src, got.Path, "the original is played when there is no proxy")
	}

	assert.Equal(t, 1, runs(t, m.ffmpegs), "ffmpeg ran for the first request only")
	assert.Equal(t, 2, runs(t, m.probes), "one probe for the height, one inside Proxy; none for the later requests")
}

func TestTakeVideo_APictureOfUnreportedHeightIsProbedOnceNotOnEveryRangeRequest(t *testing.T) {
	m := newFakeMedia(t, "1080", "exit 1\n")

	for range 3 {
		got, err := m.f.svc.Takes.Video(context.Background(), m.takeID)
		require.NoError(t, err)
		assert.Equal(t, m.src, got.Path)
	}

	assert.Equal(t, 1, runs(t, m.probes))
	assert.Equal(t, 0, runs(t, m.ffmpegs), "1080 px needs no proxy")
}

func TestTakeVideo_AClientThatGivesUpDoesNotKillTheConversion(t *testing.T) {
	// ffmpeg takes a moment and writes its output (the last argument), as the real one does.
	m := newFakeMedia(t, "2160", "for last; do :; done\nsleep 0.5\necho video > \"$last\"\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()

	got, err := m.f.svc.Takes.Video(ctx, m.takeID)
	require.NoError(t, err)
	assert.Equal(t, m.src, got.Path, "the request that gave up plays the original")

	proxy := filepath.Join(m.f.assetsDir, ".previews", "t1.mp4")
	require.Eventually(t, func() bool { _, err := os.Stat(proxy); return err == nil }, 10*time.Second, 20*time.Millisecond,
		"the conversion went on after the request ended")
	got, err = m.f.svc.Takes.Video(context.Background(), m.takeID)
	require.NoError(t, err)
	assert.Equal(t, proxy, got.Path, "the next request plays the finished proxy")
	assert.Equal(t, 1, runs(t, m.ffmpegs))
}
