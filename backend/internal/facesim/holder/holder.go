// Package holder is the face engine of the app as something that comes and goes while mam runs (Issue #1026): the models are loaded the first time they
// are used and whenever they were not there before, so that installing them from the model manager turns the face similarity on without a restart; and
// the engine can be let go of (Release) so that the files can be replaced or deleted, which Windows refuses while the models are loaded.
package holder

import (
	"context"
	"image"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// Loaded is an engine that was loaded and must be closed.
type Loaded interface {
	facesim.Engine
	Close() error
}

// Holder implements facesim.Engine (and facesim.Availability) over an engine that is loaded on demand. It is safe for concurrent use. A load that
// fails is not remembered: the next use tries again (a stat of the two files while they are missing), so a file that was put there, or the ONNX
// Runtime library that was installed, is noticed by the next measurement.
type Holder struct {
	load     func() (Loaded, error)
	diagnose func(err error) *facesim.Off
	// use is held shared while the engine infers and exclusive while it is released: Release waits for the measurements in flight, and they never
	// run on an engine that was closed.
	use    sync.RWMutex
	mu     sync.Mutex // guards eng and closed
	eng    Loaded
	closed bool
}

// New returns a Holder that loads the engine with load. diagnose says why a load failed (facesim.ONNXConfig.Diagnose for the real one).
func New(load func() (Loaded, error), diagnose func(err error) *facesim.Off) *Holder {
	return &Holder{load: load, diagnose: diagnose}
}

// get returns the engine, loading it when it is not loaded. The caller holds use (shared or exclusive).
func (h *Holder) get() (Loaded, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, facesim.ErrUnavailable
	}
	if h.eng != nil {
		return h.eng, nil
	}
	e, err := h.load()
	if err != nil {
		return nil, err
	}
	h.eng = e
	return e, nil
}

// Faces implements facesim.Engine; it answers an error wrapping facesim.ErrUnavailable while the engine cannot be loaded.
func (h *Holder) Faces(ctx context.Context, img image.Image) ([]facesim.Face, error) {
	h.use.RLock()
	defer h.use.RUnlock()
	e, err := h.get()
	if err != nil {
		return nil, err
	}
	return e.Faces(ctx, img)
}

// Off is why the engine is off now, or nil when it works (loading it when it is not loaded yet).
func (h *Holder) Off() *facesim.Off {
	h.use.RLock()
	defer h.use.RUnlock()
	if _, err := h.get(); err != nil {
		return h.diagnose(err)
	}
	return nil
}

// Available implements facesim.Availability.
func (h *Holder) Available() bool { return h.Off() == nil }

// Release lets go of the engine (waiting for the measurements in flight), runs fn while nothing can use it (replace or delete the model files here),
// and leaves the engine to be loaded again by the next use. A nil fn only lets go. fn's error is returned.
func (h *Holder) Release(fn func() error) error {
	h.use.Lock()
	defer h.use.Unlock()
	_ = h.drop() // the engine is gone either way; what fn cannot do with a file that is still held says so itself
	if fn == nil {
		return nil
	}
	return fn()
}

// Close lets go of the engine for good: later uses answer facesim.ErrUnavailable.
func (h *Holder) Close() error {
	h.use.Lock()
	defer h.use.Unlock()
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	return h.drop()
}

func (h *Holder) drop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.eng == nil {
		return nil
	}
	err := h.eng.Close()
	h.eng = nil
	return err
}
