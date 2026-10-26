package appserver

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/rpc"
)

// AccountListener receives the notifications about the account (login completed, account updated, rate limits updated).
// It runs on the reading goroutine: it must not block, and must not call the server (start a goroutine for that).
type AccountListener func(method string, params json.RawMessage)

// handler is the rpc.Handler of one process: it routes what the server sends and refuses what it asks.
type handler struct {
	logger *slog.Logger
	listen AccountListener

	mu   sync.Mutex
	subs map[string]*Sub // by thread id
}

func newHandler(logger *slog.Logger, listen AccountListener) *handler {
	return &handler{logger: logger, listen: listen, subs: map[string]*Sub{}}
}

func (h *handler) subscribe(threadID string) *Sub {
	s := newSub()
	h.mu.Lock()
	h.subs[threadID] = s
	h.mu.Unlock()
	return s
}

func (h *handler) unsubscribe(threadID string) {
	h.mu.Lock()
	s := h.subs[threadID]
	delete(h.subs, threadID)
	h.mu.Unlock()
	if s != nil {
		s.mu.Lock()
		s.closed = true
		s.queue = nil
		s.mu.Unlock()
	}
}

func (h *handler) sub(threadID string) *Sub {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subs[threadID]
}

// threadOf reads the threadId of a message without decoding the rest of it.
func threadOf(params json.RawMessage) string {
	var p struct {
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(params, &p)
	return p.ThreadID
}

// Notification implements rpc.Handler.
func (h *handler) Notification(method string, params json.RawMessage) {
	switch method {
	case protocol.NotifLoginCompleted, protocol.NotifAccountUpdated, protocol.NotifRateLimits:
		if h.listen != nil {
			h.listen(method, params)
		}
	case protocol.NotifAgentDelta, protocol.NotifItemCompleted, protocol.NotifTurnCompleted, protocol.NotifError,
		"item/started", "turn/started":
		if s := h.sub(threadOf(params)); s != nil {
			s.push(Event{Method: method, Params: params})
		}
	}
}

// Request implements rpc.Handler: every request is refused. The log line names the method and the thread only.
func (h *handler) Request(method string, params json.RawMessage) (any, *rpc.Error) {
	thread := threadOf(params)
	if s := h.sub(thread); s != nil {
		s.countRefusal()
	}
	h.logger.Warn("codex app-server request refused", "method", method, "thread", thread)
	if answer, ok := protocol.Refusal(method); ok {
		return answer, nil
	}
	return nil, &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "mam does not support " + method}
}
