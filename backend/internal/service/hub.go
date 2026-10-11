package service

import "sync"

// EventType is the kind of a project event sent over WebSocket /ws/projects/:id.
type EventType string

// Project events. They only say that something changed; the client fetches the
// HTML fragment itself (spec/architecture/overview.md §7), so no body travels over the socket.
const (
	// EventMessageCreated: new messages exist after the client's last message.
	EventMessageCreated EventType = "message.created"
	// EventCardUpdated: the card MessageID changed in place (progress, approval answered).
	EventCardUpdated EventType = "card.updated"
)

// Event is one notification. MessageID is the newest message (message.created) or the card that changed (card.updated).
type Event struct {
	Type      EventType `json:"type"`
	MessageID string    `json:"message_id"`
}

// subscriberBuffer bounds how many events a slow client may lag behind. Events are
// only hints to refetch, so when the buffer is full the oldest pending event is
// dropped; the newest one always survives.
const subscriberBuffer = 16

// broker fans events of type T out to the subscribers of a key (a Project or a Shot).
type broker[T any] struct {
	mu   sync.Mutex
	subs map[string]map[chan T]struct{}
}

func newBroker[T any]() *broker[T] { return &broker[T]{subs: make(map[string]map[chan T]struct{})} }

// Subscribe returns a channel of the key's events and a function that ends the subscription.
func (h *broker[T]) Subscribe(key string) (<-chan T, func()) {
	ch, cancel := h.subscribe(key)
	return ch, cancel
}

// subscribe is Subscribe with the send side of the channel, so the subscriber's own snapshot can be
// queued into it (sendTo).
func (h *broker[T]) subscribe(key string) (chan T, func()) {
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

// sendTo queues e into the subscription ch only, behind what it already holds; with onlyIfEmpty it
// is skipped when an event has arrived already (that one is newer than a snapshot taken after the
// subscription began). It runs under the broker lock, so no Publish can interleave with the check.
func (h *broker[T]) sendTo(ch chan T, e T, onlyIfEmpty bool) {
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
func (h *broker[T]) Publish(key string, e T) {
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

// Hub fans project events out to subscribers (the WebSocket connections).
type Hub struct{ *broker[Event] }

// NewHub returns an empty Hub.
func NewHub() *Hub { return &Hub{newBroker[Event]()} }
