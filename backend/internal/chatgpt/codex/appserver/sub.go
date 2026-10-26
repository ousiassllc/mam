package appserver

import (
	"context"
	"encoding/json"
	"sync"
)

// Event is a notification of a thread.
type Event struct {
	Method string
	Params json.RawMessage
}

// Sub receives the events of one thread, in order, with no limit (the reading goroutine of the connection never blocks
// on a slow turn). Events arrive until Close.
type Sub struct {
	mu     sync.Mutex
	queue  []Event
	signal chan struct{}
	closed bool
	// refused counts the requests of the server that were refused for this thread.
	refused int
}

func newSub() *Sub { return &Sub{signal: make(chan struct{}, 1)} }

func (s *Sub) push(e Event) {
	s.mu.Lock()
	if !s.closed {
		s.queue = append(s.queue, e)
	}
	s.mu.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// Next returns the next event, waiting for it. ok is false when ctx ended or done was closed first.
func (s *Sub) Next(ctx context.Context, done <-chan struct{}) (e Event, ok bool) {
	for {
		s.mu.Lock()
		if len(s.queue) > 0 {
			e, s.queue = s.queue[0], s.queue[1:]
			s.mu.Unlock()
			return e, true
		}
		s.mu.Unlock()
		select {
		case <-s.signal:
		case <-ctx.Done():
			return Event{}, false
		case <-done:
			// Events that arrived before the end of the process are still delivered.
			s.mu.Lock()
			if len(s.queue) > 0 {
				e, s.queue = s.queue[0], s.queue[1:]
				s.mu.Unlock()
				return e, true
			}
			s.mu.Unlock()
			return Event{}, false
		}
	}
}

// Refused is how many requests of the server (approvals, tool calls) were refused for this thread so far.
func (s *Sub) Refused() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

func (s *Sub) countRefusal() {
	s.mu.Lock()
	s.refused++
	s.mu.Unlock()
}
