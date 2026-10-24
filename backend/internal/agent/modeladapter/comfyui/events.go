package comfyui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/coder/websocket"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/progress"
)

// tracker follows the WebSocket messages of one prompt: the progress it reports to the caller, and the
// moment it ends (the server's own word for it; the history is what confirms the result).
type tracker struct {
	mu       sync.Mutex
	promptID string
	classes  map[string]string // node id -> class_type
	total    int               // nodes of the graph
	cached   int               // nodes the server skipped (their output is cached)
	started  int               // nodes that started running
	current  string
	failure  string
	ctx      context.Context
	changed  chan struct{} // receives a signal when the prompt ended or failed
}

func newTracker(ctx context.Context, promptID string, graph localcfg.Graph) *tracker {
	t := &tracker{ctx: ctx, promptID: promptID, total: max(1, len(graph)), classes: map[string]string{}, changed: make(chan struct{}, 1)}
	for id, n := range graph {
		t.classes[id] = n.ClassType
	}
	return t
}

// setPrompt follows another prompt ID (a server that ignores the one the client chose answers with its own).
func (t *tracker) setPrompt(id string) {
	t.mu.Lock()
	t.promptID = id
	t.mu.Unlock()
}

// failed is the message of an execution error the server reported, "" while there is none.
func (t *tracker) failed() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failure
}

type message struct {
	Type string `json:"type"`
	Data struct {
		PromptID string   `json:"prompt_id"`
		Node     *string  `json:"node"`
		Nodes    []string `json:"nodes"`
		Value    float64  `json:"value"`
		Max      float64  `json:"max"`
		Message  string   `json:"exception_message"`
	} `json:"data"`
}

// handle applies one text message of the socket.
func (t *tracker) handle(raw []byte) {
	var m message
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	t.mu.Lock()
	if m.Data.PromptID != t.promptID {
		t.mu.Unlock()
		return // another client's job (the server broadcasts status to every socket)
	}
	update, ended := progress.Update{}, false
	switch m.Type {
	case "execution_cached":
		t.cached = len(m.Data.Nodes)
	case "executing":
		if m.Data.Node == nil {
			ended = true
			break
		}
		t.started++
		t.current = *m.Data.Node
		update = t.update(0)
	case "progress":
		if m.Data.Max > 0 {
			update = t.update(m.Data.Value / m.Data.Max)
		}
	case "execution_error":
		t.failure = strings.TrimSpace(m.Data.Message)
		if t.failure == "" {
			t.failure = "the workflow failed"
		}
		ended = true
	case "execution_interrupted":
		t.failure = "the job was interrupted on the server"
		ended = true
	}
	t.mu.Unlock()
	if update.Message != "" {
		progress.Report(t.ctx, update)
	}
	if ended {
		select {
		case t.changed <- struct{}{}:
		default:
		}
	}
}

// update is the progress at a point inside the current node: the nodes done so far (the cached ones and
// those that ran before this one) and how far this one is. The caller holds t.mu.
func (t *tracker) update(within float64) progress.Update {
	done := float64(t.cached + max(0, t.started-1))
	pct := int(100 * (done + within) / float64(t.total))
	return progress.Update{Percent: min(max(pct, 0), 99), Message: "ComfyUI: " + t.classes[t.current]}
}

// listen opens the server's WebSocket for clientID and feeds t until ctx ends or the socket closes. A server
// without a usable socket costs only the progress: the history is polled either way. stop closes the socket.
func (b *Backend) listen(ctx context.Context, clientID string, t *tracker) (stop func()) {
	target := b.Client.Base + "/ws?clientId=" + url.QueryEscape(clientID)
	header, err := b.Client.Auth(target)
	if err != nil || b.Client.Sockets == nil {
		return func() {}
	}
	conn, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPClient: b.Client.Sockets, HTTPHeader: toHeader(header)})
	if err != nil {
		return func() {}
	}
	conn.SetReadLimit(1 << 20)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if kind == websocket.MessageText {
				t.handle(data)
			}
		}
	}()
	return func() {
		_ = conn.CloseNow()
		<-done
	}
}

func toHeader(m map[string]string) http.Header {
	h := http.Header{}
	for k, v := range m {
		h.Set(k, v)
	}
	return h
}
