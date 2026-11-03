package apprestart_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
)

// rig is a Service over a front that counts the restarts and a list of work that the test sets.
type rig struct {
	svc      *apprestart.Service
	restarts atomic.Int32
	restart  chan struct{} // closed on the first restart
	pending  *restartpending.Registry

	mu     sync.Mutex
	busy   []apprestart.Busy
	tick   chan struct{} // one value releases one wait of a waiting request
	asleep chan struct{} // the waiting request tells it is about to wait (so the work it just looked at is settled)
}

func newRig(t *testing.T, withRestart bool) *rig {
	t.Helper()
	r := &rig{pending: restartpending.New(), restart: make(chan struct{}), tick: make(chan struct{}), asleep: make(chan struct{})}
	d := apprestart.Deps{
		Busy: func(context.Context) []apprestart.Busy {
			r.mu.Lock()
			defer r.mu.Unlock()
			return append([]apprestart.Busy(nil), r.busy...)
		},
		Pending: r.pending, BootID: "boot-1", StartedAt: time.Unix(1_000, 0),
		Sleep: func(ctx context.Context, _ time.Duration) error {
			select {
			case r.asleep <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case <-r.tick:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	if withRestart {
		d.Restart = func() {
			if r.restarts.Add(1) == 1 {
				close(r.restart)
			}
		}
	}
	r.svc = apprestart.New(d)
	t.Cleanup(r.svc.Close)
	return r
}

func (r *rig) setBusy(b ...apprestart.Busy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.busy = b
}

var (
	jobs     = apprestart.Busy{Kind: apprestart.KindJobs, Message: "生成などのジョブ 1 件"}
	download = apprestart.Busy{Kind: apprestart.KindDownload, Message: "モデルの導入 1 件"}
)

func TestRequest_RestartsAtOnceWhenNothingRuns(t *testing.T) {
	r := newRig(t, true)

	res, err := r.svc.Request(t.Context(), apprestart.Options{})
	require.NoError(t, err)

	assert.Equal(t, apprestart.StateRestarting, res.State)
	assert.Equal(t, "boot-1", res.BootID)
	assert.EqualValues(t, 1, r.restarts.Load())
	again, err := r.svc.Request(t.Context(), apprestart.Options{})
	require.NoError(t, err)
	assert.Equal(t, apprestart.StateRestarting, again.State)
	assert.EqualValues(t, 1, r.restarts.Load(), "a second request while quitting does not ask the front twice")
}

func TestRequest_RefusesWhileWorkRunsAndChangesNothing(t *testing.T) {
	r := newRig(t, true)
	r.setBusy(jobs)

	_, err := r.svc.Request(t.Context(), apprestart.Options{WhenBusy: apprestart.WhenBusyRefuse})

	var busy *apprestart.BusyError
	require.ErrorAs(t, err, &busy)
	assert.Equal(t, []apprestart.Busy{jobs}, busy.Busy)
	assert.False(t, busy.StopRefused)
	assert.Contains(t, err.Error(), "生成などのジョブ 1 件")
	assert.Zero(t, r.restarts.Load())
	assert.False(t, r.svc.Status().Restarting)
}

func TestRequest_StopRestartsAnywayButNeverCutsADownload(t *testing.T) {
	r := newRig(t, true)
	r.setBusy(jobs, download)

	_, err := r.svc.Request(t.Context(), apprestart.Options{WhenBusy: apprestart.WhenBusyStop})
	var busy *apprestart.BusyError
	require.ErrorAs(t, err, &busy)
	assert.True(t, busy.StopRefused, "a download is not cut, even when asked to stop")
	assert.Contains(t, err.Error(), "ダウンロード")
	assert.Zero(t, r.restarts.Load())

	r.setBusy(jobs)
	res, err := r.svc.Request(t.Context(), apprestart.Options{WhenBusy: apprestart.WhenBusyStop})
	require.NoError(t, err)
	assert.Equal(t, apprestart.StateRestarting, res.State)
	assert.Equal(t, []apprestart.Busy{jobs}, res.Busy, "what was interrupted is told")
	assert.EqualValues(t, 1, r.restarts.Load())
}

func TestRequest_WaitRestartsWhenTheWorkEnds(t *testing.T) {
	r := newRig(t, true)
	r.setBusy(jobs)

	res, err := r.svc.Request(t.Context(), apprestart.Options{WhenBusy: apprestart.WhenBusyWait, WaitFor: time.Hour})
	require.NoError(t, err)
	assert.Equal(t, apprestart.StateWaiting, res.State)
	assert.False(t, res.WaitUntil.IsZero())
	assert.True(t, r.svc.Status().Waiting)

	joined, err := r.svc.Request(t.Context(), apprestart.Options{WhenBusy: apprestart.WhenBusyWait})
	require.NoError(t, err)
	assert.Equal(t, res.WaitUntil, joined.WaitUntil, "a second request joins the wait that is on")

	<-r.asleep
	r.tick <- struct{}{} // a look: still running
	<-r.asleep           // it looked and went back to waiting
	assert.Zero(t, r.restarts.Load())
	r.setBusy()
	r.tick <- struct{}{} // a look: idle
	<-r.restart
	assert.EqualValues(t, 1, r.restarts.Load())
	assert.False(t, r.svc.Status().Waiting)
	assert.True(t, r.svc.Status().Restarting)
}

func TestRequest_WaitGivesUpWithoutRestartingWhenClosed(t *testing.T) {
	r := newRig(t, true)
	r.setBusy(jobs)
	_, err := r.svc.Request(t.Context(), apprestart.Options{WhenBusy: apprestart.WhenBusyWait})
	require.NoError(t, err)
	<-r.asleep

	r.svc.Close() // waits for the goroutine: it ended on the cancelled context

	assert.Zero(t, r.restarts.Load())
	assert.False(t, r.svc.Status().Waiting)
}

func TestRequest_ABuildThatCannotRestartSaysSoAndRejectsUnknownChoices(t *testing.T) {
	r := newRig(t, false)
	assert.False(t, r.svc.Available())
	_, err := r.svc.Request(t.Context(), apprestart.Options{})
	assert.ErrorIs(t, err, apprestart.ErrUnavailable)

	r = newRig(t, true)
	_, err = r.svc.Request(t.Context(), apprestart.Options{WhenBusy: "later"})
	assert.Error(t, err)
	assert.Zero(t, r.restarts.Load())
}

func TestStatus_ListsWhatWaitsForARestart(t *testing.T) {
	r := newRig(t, true)
	r.pending.Set("ffmpeg", []string{"ffmpeg のパス"})

	st := r.svc.Status()

	assert.Equal(t, []string{"ffmpeg のパス"}, st.Pending)
	assert.True(t, st.Available)
	assert.Equal(t, "boot-1", st.BootID)
	assert.Equal(t, time.Unix(1_000, 0), st.StartedAt)
}
