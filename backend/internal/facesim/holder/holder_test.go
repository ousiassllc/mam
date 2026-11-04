package holder_test

import (
	"context"
	"errors"
	"image"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/facesim/holder"
)

// engine is a loaded engine whose Faces can be held in flight and whose Close is counted.
type engine struct {
	entered chan struct{} // closed when the first Faces call started
	release chan struct{} // Faces returns once it is closed (nil: at once)
	once    sync.Once
	closed  atomic.Int32
	// usedAfterClose counts Faces calls that started after Close, or were still running at Close: they must never happen. It is shared by every engine of a rig.
	usedAfterClose *atomic.Int32
}

func (e *engine) Faces(context.Context, image.Image) ([]facesim.Face, error) {
	if e.closed.Load() > 0 {
		e.usedAfterClose.Add(1)
	}
	e.once.Do(func() { close(e.entered) })
	if e.release != nil {
		<-e.release
	}
	if e.closed.Load() > 0 {
		e.usedAfterClose.Add(1) // closed while it was in flight
	}
	return []facesim.Face{{Score: 1, Embedding: facesim.Embedding{1}}}, nil
}

func (e *engine) Close() error { e.closed.Add(1); return nil }

// rig is a Holder over a load that fails until ready is set.
type rig struct {
	h              *holder.Holder
	ready          atomic.Bool
	loads          atomic.Int32
	usedAfterClose atomic.Int32
}

// newRig is a Holder whose first load is e (a fresh engine for every load after a release: a closed engine is never loaded again).
func newRig(t testing.TB, e *engine) *rig {
	t.Helper()
	r := &rig{}
	e.usedAfterClose = &r.usedAfterClose
	r.h = holder.New(func() (holder.Loaded, error) {
		if !r.ready.Load() {
			return nil, errors.Join(facesim.ErrUnavailable, errors.New("model not there"))
		}
		if r.loads.Add(1) == 1 {
			return e, nil
		}
		return &engine{entered: make(chan struct{}), usedAfterClose: &r.usedAfterClose}, nil
	}, func(err error) *facesim.Off {
		return &facesim.Off{Reason: facesim.ReasonModelsMissing, Detail: err.Error()}
	})
	t.Cleanup(func() { _ = r.h.Close() })
	return r
}

func newEngine() *engine { return &engine{entered: make(chan struct{})} }

var img = image.NewGray(image.Rect(0, 0, 4, 4))

// Issue #1026: the engine is off while its load fails and on the moment it can load: no restart, and a failed load is not remembered.
func TestAnEngineThatCouldNotLoadLoadsAfterTheModelsAppear(t *testing.T) {
	r := newRig(t, newEngine())

	_, err := r.h.Faces(context.Background(), img)
	require.ErrorIs(t, err, facesim.ErrUnavailable)
	assert.False(t, r.h.Available())
	require.NotNil(t, r.h.Off())
	assert.Equal(t, facesim.ReasonModelsMissing, r.h.Off().Reason)
	assert.False(t, facesim.Usable(r.h))
	assert.Zero(t, r.loads.Load())

	r.ready.Store(true) // the models are installed

	faces, err := r.h.Faces(context.Background(), img)
	require.NoError(t, err)
	assert.Len(t, faces, 1)
	assert.Nil(t, r.h.Off())
	assert.True(t, facesim.Usable(r.h))
	assert.Equal(t, int32(1), r.loads.Load(), "loaded once and kept")
}

// Release waits for the measurement in flight, closes the engine, runs fn while nothing can use it, and the next use loads again.
func TestReleaseWaitsForTheMeasurementInFlightAndNothingUsesAClosedEngine(t *testing.T) {
	e := newEngine()
	e.release = make(chan struct{})
	r := newRig(t, e)
	r.ready.Store(true)

	faces := make(chan error, 1)
	go func() { _, err := r.h.Faces(context.Background(), img); faces <- err }()
	<-e.entered

	released := make(chan struct{})
	ran := make(chan struct{})
	go func() {
		_ = r.h.Release(func() error { close(ran); return nil })
		close(released)
	}()
	close(e.release)
	require.NoError(t, <-faces)
	<-released
	<-ran
	assert.Equal(t, int32(1), e.closed.Load())
	assert.Zero(t, r.usedAfterClose.Load())

	_, err := r.h.Faces(context.Background(), img)
	require.NoError(t, err)
	assert.Equal(t, int32(2), r.loads.Load(), "after a release the next use loads the engine again")
}

// fn of Release runs with the engine closed, and its error is the answer.
func TestReleaseRunsItsFunctionAfterTheEngineIsClosed(t *testing.T) {
	e := newEngine()
	r := newRig(t, e)
	r.ready.Store(true)
	require.True(t, r.h.Available())

	boom := errors.New("cannot delete")
	err := r.h.Release(func() error {
		assert.Equal(t, int32(1), e.closed.Load(), "closed before fn runs")
		return boom
	})
	require.ErrorIs(t, err, boom)
}

// Close is for good.
func TestUsesAfterCloseAreUnavailable(t *testing.T) {
	r := newRig(t, newEngine())
	r.ready.Store(true)
	require.True(t, r.h.Available())

	require.NoError(t, r.h.Close())

	_, err := r.h.Faces(context.Background(), img)
	require.ErrorIs(t, err, facesim.ErrUnavailable)
	assert.False(t, r.h.Available())
}

// Concurrent measurements, status reads and releases do not race and never touch a closed engine (run with -race).
func TestConcurrentUseAndReleaseNeverUseAClosedEngine(t *testing.T) {
	e := newEngine()
	r := newRig(t, e)
	r.ready.Store(true)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_, _ = r.h.Faces(context.Background(), img)
				_ = r.h.Available()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			_ = r.h.Release(nil)
		}
	}()
	wg.Wait()
	assert.Zero(t, r.usedAfterClose.Load())
}
