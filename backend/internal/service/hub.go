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

// Hub fans project events out to subscribers (the WebSocket connections).
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// NewHub returns an empty Hub.
func NewHub() *Hub { return &Hub{subs: make(map[string]map[chan Event]struct{})} }

// Subscribe returns a channel of the Project's events and a function that ends the subscription.
func (h *Hub) Subscribe(projectID string) (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	if h.subs[projectID] == nil {
		h.subs[projectID] = make(map[chan Event]struct{})
	}
	h.subs[projectID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.subs[projectID], ch)
			if len(h.subs[projectID]) == 0 {
				delete(h.subs, projectID)
			}
		})
	}
}

// Publish delivers e to every subscriber of the Project without blocking.
func (h *Hub) Publish(projectID string, e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[projectID] {
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
