// Package broker is the fan-out of the events of the service layer: a Project's chat events (service.Hub) and a Shot's progress. It is a package of its own
// so that service stays within its size (linterly).
package broker

import "sync"

// subscriberBuffer bounds how many events a slow client may lag behind. Events are
// only hints to refetch, so when the buffer is full the oldest pending event is
// dropped; the newest one always survives.
const subscriberBuffer = 16

// Broker fans events of type T out to the subscribers of a key (a Project or a Shot).
type Broker[T any] struct {
	mu   sync.Mutex
	subs map[string]map[chan T]struct{}
}

// New returns an empty Broker.
func New[T any]() *Broker[T] { return &Broker[T]{subs: make(map[string]map[chan T]struct{})} }

// Subscribe returns a channel of the key's events and a function that ends the subscription.
func (h *Broker[T]) Subscribe(key string) (<-chan T, func()) {
	ch, cancel := h.SubscribeSend(key)
	return ch, cancel
}

// SubscribeSend is Subscribe with the send side of the channel, so the subscriber's own snapshot can be
// queued into it (SendTo).
func (h *Broker[T]) SubscribeSend(key string) (chan T, func()) {
	ch := make(chan T, subscriberBuffer)
	h.mu.Lock()
	if h.subs[key] == nil {
		h.subs[key] = make(map[chan T]struct{})
	}
	h.subs[key][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.subs[key], ch)
			if len(h.subs[key]) == 0 {
				delete(h.subs, key)
			}
		})
	}
}

// SendTo queues e into the subscription ch only, behind what it already holds; with onlyIfEmpty it
// is skipped when an event has arrived already (that one is newer than a snapshot taken after the
// subscription began). It runs under the broker lock, so no Publish can interleave with the check.
func (h *Broker[T]) SendTo(ch chan T, e T, onlyIfEmpty bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if onlyIfEmpty && len(ch) > 0 {
		return
	}
	select {
	case ch <- e:
	default: // full of newer events: the snapshot is stale by now
	}
}

// Publish delivers e to every subscriber of the key without blocking.
func (h *Broker[T]) Publish(key string, e T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[key] {
		select {
		case ch <- e:
		default: // lagging client: make room by dropping its oldest hint
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- e:
			default:
			}
		}
	}
}
