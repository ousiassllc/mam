package chatops

import (
	"context"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

// Runtime holds the chat operations the app wired: the handler of each operation type, the offers for files, the sources of the progress line. The chat reads it on every
// message while the app runs; it is filled once at start (service/chatwire) or by a test. It is safe for concurrent use.
type Runtime struct {
	mu       sync.RWMutex
	handlers map[commandinterpreter.OpType]Handler
	files    []FileOffer
	progress []ProgressDetail
}

// NewRuntime returns an empty Runtime.
func NewRuntime() *Runtime { return &Runtime{handlers: map[commandinterpreter.OpType]Handler{}} }

// Handle sets the handler of an operation.
func (r *Runtime) Handle(t commandinterpreter.OpType, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[t] = h
}

// Files adds an offer for the files of a message that no attachment of the Brief takes.
func (r *Runtime) Files(o FileOffer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files = append(r.files, o)
}

// Progress adds what says how far the job behind a progress card is.
func (r *Runtime) Progress(p ProgressDetail) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progress = append(r.progress, p)
}

// Handler is the handler of an operation; false when the app did not wire it.
func (r *Runtime) Handler(t commandinterpreter.OpType) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[t]
	return h, ok
}

// OfferFor is the offer that takes the file with this name; false when none does.
func (r *Runtime) OfferFor(name string) (FileOffer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, o := range r.files {
		if o.Accepts(name) {
			return o, true
		}
	}
	return nil, false
}

// Detail says how far the job behind a progress card is ("" when no source knows it).
func (r *Runtime) Detail(ctx context.Context, jobID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.progress {
		if text := p(ctx, jobID); text != "" {
			return text
		}
	}
	return ""
}
