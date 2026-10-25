package codextest

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// Say is the default script: one agent message and a completed turn.
func Say(text string) func(*Turn) {
	return func(t *Turn) {
		t.Delta(text)
		t.Final(text)
		t.Complete(protocol.TurnCompleted, nil)
	}
}

// Delta sends an item/agentMessage/delta.
func (t *Turn) Delta(text string) {
	t.F.Notify(protocol.NotifAgentDelta, map[string]any{"threadId": t.ThreadID, "turnId": t.TurnID, "itemId": "item-1", "delta": text})
}

// Final sends the completed final-answer agent message.
func (t *Turn) Final(text string) {
	t.Item(map[string]any{"type": protocol.ItemAgentMessage, "id": "item-1", "text": text, "phase": protocol.PhaseFinalAnswer})
}

// Item sends an item/completed.
func (t *Turn) Item(item map[string]any) {
	t.F.Notify(protocol.NotifItemCompleted, map[string]any{"threadId": t.ThreadID, "turnId": t.TurnID, "item": item})
}

// ItemStarted sends an item/started.
func (t *Turn) ItemStarted(item map[string]any) {
	t.F.Notify("item/started", map[string]any{"threadId": t.ThreadID, "turnId": t.TurnID, "item": item})
}

// Complete sends turn/completed with the status and, for a failure, the error (a TurnError JSON object such as
// {"message":"m","codexErrorInfo":"usageLimitExceeded"}).
func (t *Turn) Complete(status string, turnErr map[string]any) {
	turn := map[string]any{"id": t.TurnID, "status": status, "items": []any{}}
	if turnErr != nil {
		turn["error"] = turnErr
	}
	t.F.Notify(protocol.NotifTurnCompleted, map[string]any{"threadId": t.ThreadID, "turn": turn})
}

// ErrorNotification sends the error notification (willRetry says the server retries by itself).
func (t *Turn) ErrorNotification(turnErr map[string]any, willRetry bool) {
	t.F.Notify(protocol.NotifError, map[string]any{"threadId": t.ThreadID, "turnId": t.TurnID, "willRetry": willRetry, "error": turnErr})
}

// Ask sends a request of the server (an approval) to the client and returns its answer.
func (t *Turn) Ask(method string, params map[string]any) (answer json.RawMessage, rpcErr *RPCError) {
	params["threadId"] = t.ThreadID
	params["turnId"] = t.TurnID
	return t.F.Ask(method, params)
}
