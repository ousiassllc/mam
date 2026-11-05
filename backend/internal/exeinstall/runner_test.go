package exeinstall_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

var (
	errClosed   = errors.New("closed")
	errRemoving = errors.New("removing")
)

// held is an Install that reports one step, then waits until it is let go (release) or cancelled.
type held struct {
	reported chan struct{}
	release  chan error
}

func newHeld() *held { return &held{reported: make(chan struct{}), release: make(chan error, 1)} }

func (h *held) install(ctx context.Context, report func(exeinstall.Status)) error {
	report(exeinstall.Status{Phase: "extract", Percent: 40, Done: 4, Total: 10, Message: "extracting"})
	close(h.reported)
	select {
	case err := <-h.release:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *held) waitReported(t *testing.T) {
	t.Helper()
	select {
	case <-h.reported:
	case <-time.After(5 * time.Second):
		t.Fatal("the install did not report")
	}
}

func newRunner(install func(context.Context, func(exeinstall.Status)) error, preflight func() error) *exeinstall.Runner {
	return exeinstall.NewRunner(exeinstall.RunnerConfig{
		Kind:        worktrack.KindFFmpegInstall,
		Subject:     "tool",
		Starting:    exeinstall.Status{Phase: "download", Message: "downloading"},
		ErrClosed:   errClosed,
		ErrRemoving: errRemoving,
		Preflight:   preflight,
		Install:     install,
		Succeeded:   "done",
		Cancelled:   exeinstall.Status{Error: "stopped"},
		Failed:      func(err error) string { return "failed: " + err.Error() },
	})
}

func TestRunnerIsIdleUntilStarted(t *testing.T) {
	r := newRunner(newHeld().install, nil)
	assert.Equal(t, exeinstall.Status{State: exeinstall.Idle}, r.Status())
}

func TestRunnerStartsOnceAndReportsTheProgressUntilItSucceeds(t *testing.T) {
	h := newHeld()
	r := newRunner(h.install, nil)

	started, err := r.Start()
	require.NoError(t, err)
	assert.True(t, started)
	h.waitReported(t)
	assert.Equal(t, exeinstall.Status{State: exeinstall.Running, Phase: "extract", Percent: 40, Done: 4, Total: 10, Message: "extracting"}, r.Status())

	started, err = r.Start()
	require.NoError(t, err)
	assert.False(t, started, "an install that runs is not started again")

	h.release <- nil
	r.Wait()
	assert.Equal(t, exeinstall.Status{State: exeinstall.Succeeded, Percent: 100, Message: "done"}, r.Status())
	works := r.Works()
	require.Len(t, works, 1)
	assert.Equal(t, worktrack.Succeeded, works[0].State)
}

func TestRunnerStartShowsTheStartingStatusWhilePreflightRuns(t *testing.T) {
	var r *exeinstall.Runner
	var during exeinstall.Status
	r = newRunner(newHeld().install, func() error { during = r.Status(); return errors.New("no room") })

	_, err := r.Start()
	require.EqualError(t, err, "no room")
	assert.Equal(t, exeinstall.Status{State: exeinstall.Running, Phase: "download", Message: "downloading"}, during, "the program must see an install under way")
	assert.Equal(t, exeinstall.Status{State: exeinstall.Idle}, r.Status(), "a refusal leaves the status as it was")
	assert.Empty(t, r.Works(), "a refusal is no work")

	// the refusal released the install: a start that passes goes on
	r2 := newRunner(func(context.Context, func(exeinstall.Status)) error { return nil }, func() error { return nil })
	started, err := r2.Start()
	require.NoError(t, err)
	assert.True(t, started)
	r2.Wait()
}

func TestRunnerCancelEndsItAsCancelledAndInstallingAgainRuns(t *testing.T) {
	h := newHeld()
	r := newRunner(h.install, nil)
	_, err := r.Start()
	require.NoError(t, err)
	h.waitReported(t)

	r.Cancel()
	r.Wait()
	assert.Equal(t, exeinstall.Status{State: exeinstall.Failed, Error: "stopped", Cancelled: true}, r.Status())
	assert.Equal(t, worktrack.Cancelled, r.Works()[0].State)

	h2 := newHeld()
	r2 := newRunner(h2.install, nil)
	_, err = r2.Start()
	require.NoError(t, err)
	h2.waitReported(t)
	h2.release <- nil
	r2.Wait()
}

func TestRunnerFailureIsWordedByTheProgram(t *testing.T) {
	h := newHeld()
	r := newRunner(h.install, nil)
	_, err := r.Start()
	require.NoError(t, err)
	h.waitReported(t)

	h.release <- errors.New("boom")
	r.Wait()
	assert.Equal(t, exeinstall.Status{State: exeinstall.Failed, Error: "failed: boom"}, r.Status())
	assert.Equal(t, worktrack.Failed, r.Works()[0].State)
}

func TestRunnerHoldRefusesStartsUntilReleased(t *testing.T) {
	r := newRunner(func(context.Context, func(exeinstall.Status)) error { return nil }, nil)
	release, ok := r.Hold(true)
	require.True(t, ok)
	_, again := r.Hold(true)
	assert.False(t, again, "an exclusive hold is refused while another is in force")

	started, err := r.Start()
	assert.False(t, started)
	assert.ErrorIs(t, err, errRemoving)

	release()
	started, err = r.Start()
	require.NoError(t, err)
	assert.True(t, started)
	r.Wait()
}

func TestRunnerExclusiveHoldIsRefusedWhileAnInstallRuns(t *testing.T) {
	h := newHeld()
	r := newRunner(h.install, nil)
	_, err := r.Start()
	require.NoError(t, err)
	h.waitReported(t)

	_, ok := r.Hold(true)
	assert.False(t, ok, "the files are never deleted under a running install")
	release, ok := r.Hold(false)
	assert.True(t, ok, "the caller of a non-exclusive hold stops the install itself")
	release()

	h.release <- nil
	r.Wait()
}

func TestRunnerResetForgetsTheEndOfTheLastInstall(t *testing.T) {
	r := newRunner(func(context.Context, func(exeinstall.Status)) error { return nil }, nil)
	_, err := r.Start()
	require.NoError(t, err)
	r.Wait()
	require.Equal(t, exeinstall.Succeeded, r.Status().State)

	r.Reset()
	assert.Equal(t, exeinstall.Status{State: exeinstall.Idle}, r.Status())
}

func TestRunnerCloseCancelsTheInstallAndRefusesNewOnes(t *testing.T) {
	h := newHeld()
	r := newRunner(h.install, nil)
	_, err := r.Start()
	require.NoError(t, err)
	h.waitReported(t)

	r.Close()
	assert.Equal(t, exeinstall.Failed, r.Status().State, "the install ended: Close waits for it")
	started, err := r.Start()
	assert.False(t, started)
	assert.ErrorIs(t, err, errClosed)
}
