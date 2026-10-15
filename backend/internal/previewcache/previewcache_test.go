package previewcache_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/previewcache"
)

func newCache() *previewcache.Cache {
	return previewcache.New(slog.New(slog.NewTextHandler(io.Discard, nil)), "", 0)
}

var errBroken = errors.New("ffmpeg: broken file")

func TestMake_AFailedFileIsNotTriedAgainWithinRetryAfter(t *testing.T) {
	c, dst := newCache(), filepath.Join(t.TempDir(), "t1.mp4")
	var runs atomic.Int32
	build := func(context.Context) error { runs.Add(1); return errBroken }

	err := c.Make(context.Background(), dst, time.Hour, build)
	assert.ErrorIs(t, err, errBroken)
	assert.NotErrorIs(t, err, previewcache.ErrFailedRecently, "the first failure is the failure itself")
	for range 3 {
		err = c.Make(context.Background(), dst, time.Hour, build)
		assert.ErrorIs(t, err, previewcache.ErrFailedRecently)
		assert.ErrorIs(t, err, errBroken, "the cause is kept")
	}

	assert.EqualValues(t, 1, runs.Load(), "build ran for the first request only")
}

func TestMake_AFailedFileIsTriedAgainAfterRetryAfterAndWithoutOne(t *testing.T) {
	c, dst := newCache(), filepath.Join(t.TempDir(), "t1.mp4")
	var runs atomic.Int32
	build := func(context.Context) error { runs.Add(1); return errBroken }

	for range 2 {
		require.ErrorIs(t, c.Make(context.Background(), dst, time.Millisecond, build), errBroken)
		time.Sleep(5 * time.Millisecond)
	}
	assert.EqualValues(t, 2, runs.Load(), "retryAfter ran out")

	for range 2 {
		require.ErrorIs(t, c.Make(context.Background(), dst, 0, build), errBroken)
	}
	assert.EqualValues(t, 4, runs.Load(), "without retryAfter nothing is remembered")
}

func TestMake_AFileThatExistsIsNotMadeAgain(t *testing.T) {
	c, dst := newCache(), filepath.Join(t.TempDir(), "t1.mp4")
	var runs atomic.Int32
	build := func(context.Context) error { runs.Add(1); return os.WriteFile(dst, []byte("x"), 0o600) }

	for range 3 {
		require.NoError(t, c.Make(context.Background(), dst, time.Hour, build))
	}

	assert.EqualValues(t, 1, runs.Load())
}

func TestMake_ARequestThatGivesUpDoesNotKillTheConversion(t *testing.T) {
	c, dst := newCache(), filepath.Join(t.TempDir(), "t1.mp4")
	started, finish := make(chan struct{}), make(chan struct{})
	var stillAlive atomic.Bool
	build := func(ctx context.Context) error {
		close(started)
		<-finish
		stillAlive.Store(ctx.Err() == nil)
		return os.WriteFile(dst, []byte("x"), 0o600)
	}
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan error, 1)
	go func() { got <- c.Make(ctx, dst, time.Hour, build) }()
	<-started

	cancel()
	assert.ErrorIs(t, <-got, context.Canceled, "the caller stops waiting")
	close(finish)

	require.Eventually(t, func() bool { _, err := os.Stat(dst); return err == nil }, 5*time.Second, 5*time.Millisecond)
	assert.True(t, stillAlive.Load(), "build's context was not the request's")
	assert.NoError(t, c.Make(context.Background(), dst, time.Hour, func(context.Context) error { t.Error("made twice"); return nil }))
}

func TestClose_CancelsTheRunningConversionAndWaitsForIt(t *testing.T) {
	c, dst := newCache(), filepath.Join(t.TempDir(), "t1.mp4")
	started, stopped := make(chan struct{}), make(chan struct{})
	build := func(ctx context.Context) error {
		close(started)
		<-ctx.Done() // ffmpeg is killed with the context
		close(stopped)
		return ctx.Err()
	}
	go func() { _ = c.Make(context.Background(), dst, time.Hour, build) }()
	<-started

	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return: build's context was not cancelled")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("Close returned before the conversion ended")
	}
}

func TestClose_RefusesFurtherConversions(t *testing.T) {
	c, dst := newCache(), filepath.Join(t.TempDir(), "t1.mp4")
	c.Close()

	err := c.Make(context.Background(), dst, time.Hour, func(context.Context) error { t.Error("built after Close"); return nil })

	assert.ErrorIs(t, err, previewcache.ErrClosed)
}

func TestMake_RequestsForOneFileWaitForOneRun(t *testing.T) {
	c, dst := newCache(), filepath.Join(t.TempDir(), "t1.mp4")
	var runs atomic.Int32
	build := func(context.Context) error {
		runs.Add(1)
		time.Sleep(50 * time.Millisecond)
		return os.WriteFile(dst, []byte("x"), 0o600)
	}
	errs := make(chan error, 4)
	for range 4 {
		go func() { errs <- c.Make(context.Background(), dst, time.Hour, build) }()
	}

	for range 4 {
		assert.NoError(t, <-errs)
	}
	assert.EqualValues(t, 1, runs.Load())
}

func TestMake_APanicIsAnErrorAndReleasesTheFile(t *testing.T) {
	c, dst := newCache(), filepath.Join(t.TempDir(), "t1.mp4")

	err := c.Make(context.Background(), dst, 0, func(context.Context) error { panic("boom") })
	require.Error(t, err)

	assert.NoError(t, c.Make(context.Background(), dst, 0, func(context.Context) error { return nil }), "the lock was released")
}

func TestTall_IsProbedOncePerIDAndAFailedProbeIsNotKept(t *testing.T) {
	c := newCache()
	var probes int
	probe := func(tall bool, err error) func() (bool, error) {
		return func() (bool, error) { probes++; return tall, err }
	}

	_, err := c.Tall("t1", probe(false, errBroken))
	assert.ErrorIs(t, err, errBroken)
	for range 3 {
		tall, err := c.Tall("t1", probe(true, nil))
		require.NoError(t, err)
		assert.True(t, tall)
	}
	tall, err := c.Tall("t2", probe(false, nil))
	require.NoError(t, err)
	assert.False(t, tall)

	assert.Equal(t, 3, probes, "t1: the failed probe and one more; t2: one")
}
