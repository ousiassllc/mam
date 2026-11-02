package localbackends_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/progress"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// These tests run the ComfyUI backend (comfyui/stall.go) against the fake server; they live here because the comfyui
// directory has no room left under the line limit of linterly.

const stall = 10 * time.Minute

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

// updates collects the progress the backend reports.
type updates struct {
	mu   sync.Mutex
	list []progress.Update
}

func (u *updates) add(x progress.Update) { u.mu.Lock(); u.list = append(u.list, x); u.mu.Unlock() }
func (u *updates) all() []progress.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]progress.Update(nil), u.list...)
}

// backend is the image backend of a ComfyUI fake, under a stall limit and a clock the test moves.
func backend(t *testing.T, f *comfyuitest.Fake, clock *manualClock) *comfyui.Backend {
	t.Helper()
	tpl, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery, c.Stall, c.Now = 10*time.Millisecond, stall, clock.Now
	return &comfyui.Backend{Client: c, Model: tpl.Model}
}

// manualClock is the clock that measures the stall: it moves only when the test says so.
type manualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *manualClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// stallRun is one image generation on a fake ComfyUI that holds its job, under a manual clock and a 10 minute stall limit.
type stallRun struct {
	t       *testing.T
	f       *comfyuitest.Fake
	clock   *manualClock
	updates updates
	result  chan error
}

// startStallRun queues a job and waits until the backend has seen the server's first messages of it, so that the
// clock is moved only after the job showed its first sign of life.
func startStallRun(t *testing.T) *stallRun {
	t.Helper()
	f := comfyuitest.New(t)
	f.Hold = make(chan struct{})
	r := &stallRun{t: t, f: f, clock: &manualClock{t: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}, result: make(chan error, 1)}
	b := backend(t, f, r.clock)
	go func() {
		_, err := b.GenerateImage(progress.With(ctx(t), r.updates.add), "", modeladapter.ImageRequest{Prompt: "a red fox", AspectRatio: "1:1"})
		r.result <- err
	}()
	require.Eventually(t, func() bool { return len(r.updates.all()) >= 2 }, 10*time.Second, time.Millisecond, "the first messages of the job")
	return r
}

// step makes the server report one more step of the job and waits until the backend has taken it.
func (r *stallRun) step(value int) {
	r.t.Helper()
	n := len(r.updates.all())
	r.f.Step(0, value, 100)
	require.Eventually(r.t, func() bool { return len(r.updates.all()) > n }, 10*time.Second, time.Millisecond)
}

// polled waits until the backend has asked the server for the state of the job a few more times, so that a limit
// that had run out would have been noticed by now.
func (r *stallRun) polled() {
	r.t.Helper()
	n := r.f.Count("GET /history/")
	require.Eventually(r.t, func() bool { return r.f.Count("GET /history/") >= n+3 }, 10*time.Second, time.Millisecond)
}

func (r *stallRun) running() bool {
	select {
	case <-r.result:
		return false
	default:
		return true
	}
}

// A job that keeps stepping runs past the stall limit as long as it likes, and ends when the server says it is done.
func TestAJobThatKeepsSteppingIsNotCutOffAndEndsWhenDone(t *testing.T) {
	r := startStallRun(t)
	for i := range 5 {
		r.clock.Advance(stall - time.Minute)
		r.step(i)
	}
	r.polled()
	require.True(t, r.running(), "45 minutes in, with a step every 9: not stalled")

	close(r.f.Hold)
	require.NoError(t, <-r.result)
	assert.Empty(t, r.f.Cancelled(), "a job that finished is not cancelled")
}

// A job that shows nothing for the limit is cancelled on the server by its own ID, and the error says it stalled.
func TestAJobThatStopsSteppingIsCutOffAndCancelled(t *testing.T) {
	r := startStallRun(t)
	r.step(1)
	r.clock.Advance(stall - time.Second)
	r.polled()
	require.True(t, r.running(), "a second short of the limit")

	r.clock.Advance(time.Second)
	err := <-r.result

	require.ErrorIs(t, err, modeladapter.ErrTimeout)
	require.ErrorIs(t, err, modeladapter.ErrAfterSubmit)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var te *localcfg.TimeoutError
	require.ErrorAs(t, err, &te)
	assert.True(t, te.Stalled)
	assert.Equal(t, stall, te.After)
	assert.Equal(t, []string{r.f.PromptID(0)}, r.f.Cancelled(), "the stalled job is stopped on the server")
	close(r.f.Hold)
}

// A job that waits in the server's queue (behind another client's job) shows nothing, and is not cut off however long it
// waits; once it leaves the queue it is held to the limit like any other.
func TestAJobWaitingInTheServersQueueIsNotCutOff(t *testing.T) {
	r := startStallRun(t)
	id := r.f.PromptID(0)
	r.f.Lock()
	r.f.Pending = []string{id}
	r.f.Unlock()

	r.clock.Advance(3 * stall)
	r.polled()
	r.clock.Advance(3 * stall)
	r.polled()
	require.True(t, r.running(), "six limits in the queue: not stalled")
	assert.Empty(t, r.f.Cancelled())

	r.f.Lock()
	r.f.Pending = nil
	r.f.Running = []string{id}
	r.f.Unlock()
	r.polled()
	require.True(t, r.running(), "it just left the queue")
	r.clock.Advance(stall)
	err := <-r.result

	var te *localcfg.TimeoutError
	require.ErrorAs(t, err, &te)
	assert.True(t, te.Stalled, "running, and silent for the limit")
	assert.Equal(t, []string{id}, r.f.Cancelled())
	close(r.f.Hold)
}

// A server without a socket reports no progress, so the limit runs from the start of the job: it is held to the timeout
// as a whole, as it was before the limit followed progress.
func TestAServerWithoutASocketIsHeldToTheTimeoutAsAWhole(t *testing.T) {
	f := comfyuitest.New(t)
	f.NoSocket, f.Hold = true, make(chan struct{})
	clock := &manualClock{t: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	b := backend(t, f, clock)
	result := make(chan error, 1)
	go func() {
		_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x", AspectRatio: "1:1"})
		result <- err
	}()
	require.Eventually(t, func() bool { return f.Count("GET /history/") >= 1 }, 10*time.Second, time.Millisecond)

	clock.Advance(stall)

	var te *localcfg.TimeoutError
	require.ErrorAs(t, <-result, &te)
	assert.True(t, te.Stalled)
	close(f.Hold)
}
