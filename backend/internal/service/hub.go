package service

import "github.com/ousiassllc/mam/backend/internal/service/broker"

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

// Hub fans project events out to subscribers (the WebSocket connections).
type Hub struct{ *broker.Broker[Event] }

// NewHub returns an empty Hub.
func NewHub() *Hub { return &Hub{broker.New[Event]()} }
