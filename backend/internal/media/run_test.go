package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hangingTools returns Tools whose ffmpeg and ffprobe never finish (a stand-in for a process that
// hangs on a broken file), with every time limit as short as the test can afford.
func hangingTools(t *testing.T) Tools {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "hang")
	require.NoError(t, os.WriteFile(exe, []byte("#!/bin/sh\nexec sleep 60\n"), 0o700)) // #nosec G306 -- a test executable
	short := 200 * time.Millisecond
	return Tools{ffmpeg: exe, ffprobe: exe, lim: limits{probe: short, loudness: short, writeBase: short, writePerSecond: 0}}
}

func requireTimedOut(t *testing.T, started time.Time, err error) {
	t.Helper()
	require.ErrorIs(t, err, ErrTimeout)
	assert.Less(t, time.Since(started), 10*time.Second, "the hung process must be killed at its limit")
}

func TestProbeTimesOut(t *testing.T) {
	tools := hangingTools(t)
	started := time.Now()
	_, err := tools.Probe(context.Background(), "video.mp4")
	requireTimedOut(t, started, err)
}

func TestAudioDurationTimesOut(t *testing.T) {
	tools := hangingTools(t)
	started := time.Now()
	_, err := tools.AudioDuration(context.Background(), "voice.wav")
	requireTimedOut(t, started, err)
}

func TestMeasureLoudnessTimesOut(t *testing.T) {
	tools := hangingTools(t)
	started := time.Now()
	_, err := tools.MeasureLoudness(context.Background(), "mix.wav", -14)
	requireTimedOut(t, started, err)
}

func TestWriteTimesOutAndLeavesNoFile(t *testing.T) {
	tools := hangingTools(t)
	dst := filepath.Join(t.TempDir(), "poster.jpg")
	started := time.Now()
	err := tools.Thumbnail(context.Background(), "video.mp4", dst, 0)
	requireTimedOut(t, started, err)
	assert.NoFileExists(t, dst)
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dst), ".poster.jpg.ffmpeg.tmp"))
}

func TestFramesTimesOut(t *testing.T) {
	tools := hangingTools(t)
	started := time.Now()
	err := tools.Frames(context.Background(), "video.mp4", filepath.Join(t.TempDir(), "%04d.jpg"), "fps=1", 4, 0)
	requireTimedOut(t, started, err)
}

func TestRunReturnsContextErrorWhenCancelled(t *testing.T) {
	tools := hangingTools(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := tools.Probe(ctx, "video.mp4")
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrTimeout)
}

func TestWriteLimitGrowsWithTheMedia(t *testing.T) {
	l := limits{writeBase: time.Minute, writePerSecond: 2 * time.Second}
	assert.Equal(t, time.Minute, l.writeLimit(0))
	assert.Equal(t, time.Minute+20*time.Second, l.writeLimit(10))
	assert.Equal(t, time.Minute, l.writeLimit(-5))
}

func TestZeroToolsUseDefaultLimits(t *testing.T) {
	assert.Equal(t, defaultLimits, Tools{}.limits())
}
