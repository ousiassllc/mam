// Package comfyuitest is a fake ComfyUI server for tests: it queues what POST /prompt gets, reports progress over
// its WebSocket, answers /history, /view, /queue and the cancel routes, and records what the backend sent.
package comfyuitest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// PNG is a minimal PNG (the 8-byte signature plus a little more is enough for sniffing).
var PNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

// Fake is a ComfyUI server: it queues what POST /prompt gets, reports progress over its WebSocket,
// answers /history and /view, and records every request the backend makes.
type Fake struct {
	t   testing.TB
	srv *httptest.Server

	mu        sync.Mutex
	sockets   map[string]*websocket.Conn // client id -> socket
	submitted []map[string]json.RawMessage
	uploads   []string // file names uploaded
	requests  []string // "METHOD path" of every request
	auths     []string // Authorization header of every request
	cancelled []string
	interrupt []string // bodies of POST /interrupt
	queueDel  []string // bodies of POST /queue
	history   map[string]string
	Hold      chan struct{} // when set, a prompt does not finish until it is closed

	// OutputFile is what the history lists for the output node ("out.png").
	OutputFile string
	// OutputNode is the node the history lists the file under (the templates: "9" for images, "58" for videos).
	OutputNode string
	OutputBody []byte
	// FailWith, when set, makes the prompt end in an execution_error with this message.
	FailWith string
	// NoSocket makes /ws answer 404.
	NoSocket bool
	// CancelStatus is the status of POST /api/jobs/<id>/cancel (0: 200).
	CancelStatus int
	// running / pending are the prompt IDs GET /queue lists.
	Running, Pending []string
	Server
	ServerMu sync.Mutex
}

// New starts a fake ComfyUI server that closes with the test.
func New(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{t: t, sockets: map[string]*websocket.Conn{}, history: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// URL is the base URL of the server.
func (f *Fake) URL() string { return f.srv.URL }

func (f *Fake) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.auths = append(f.auths, r.Header.Get("Authorization"))
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	if f.serveServer(w, r) {
		return
	}
	switch {
	case r.URL.Path == "/ws":
		f.socket(w, r)
	case r.URL.Path == "/upload/image" && r.Method == http.MethodPost:
		must(f.t, r.ParseMultipartForm(32<<20)) //nolint:gosec // a test server; the client is ours
		_, hdr, err := r.FormFile("image")
		must(f.t, err)
		f.mu.Lock()
		f.uploads = append(f.uploads, hdr.Filename)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"name": hdr.Filename, "subfolder": "", "type": "input"})
	case r.URL.Path == "/prompt" && r.Method == http.MethodPost:
		f.prompt(w, r)
	case strings.HasPrefix(r.URL.Path, "/history/"):
		id := strings.TrimPrefix(r.URL.Path, "/history/")
		f.mu.Lock()
		entry, ok := f.history[id]
		f.mu.Unlock()
		if !ok {
			_, _ = w.Write([]byte("{}"))
			return
		}
		_, _ = w.Write([]byte(`{"` + id + `":` + entry + `}`)) //nolint:gosec // a test server answering with its own canned JSON
	case r.URL.Path == "/view":
		f.mu.Lock()
		body := f.OutputBody
		f.mu.Unlock()
		switch {
		case body != nil:
		case strings.HasSuffix(r.URL.Query().Get("filename"), ".mp4"):
			body = []byte("\x00\x00\x00\x18ftypisom-fake-video")
		default:
			body = PNG
		}
		_, _ = w.Write(body)
	case r.URL.Path == "/queue" && r.Method == http.MethodGet:
		f.mu.Lock()
		defer f.mu.Unlock()
		entries := func(ids []string) [][]any {
			out := [][]any{}
			for i, id := range ids {
				out = append(out, []any{i, id, map[string]any{}})
			}
			return out
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"queue_running": entries(f.Running), "queue_pending": entries(f.Pending)})
	case r.URL.Path == "/queue" && r.Method == http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.queueDel = append(f.queueDel, string(body))
		f.mu.Unlock()
	case r.URL.Path == "/interrupt" && r.Method == http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.interrupt = append(f.interrupt, string(body))
		f.mu.Unlock()
	case strings.HasPrefix(r.URL.Path, "/api/jobs/") && strings.HasSuffix(r.URL.Path, "/cancel"):
		f.mu.Lock()
		f.cancelled = append(f.cancelled, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/cancel"))
		status := f.CancelStatus
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(`{"cancelled":true}`))
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) socket(w http.ResponseWriter, r *http.Request) {
	if f.NoSocket {
		http.NotFound(w, r)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	id := r.URL.Query().Get("clientId")
	f.mu.Lock()
	f.sockets[id] = c
	f.mu.Unlock()
	// Reads are needed to process the client's close.
	for {
		if _, _, err := c.Read(r.Context()); err != nil {
			return
		}
	}
}

func (f *Fake) send(clientID, typ string, data map[string]any) {
	f.mu.Lock()
	c := f.sockets[clientID]
	f.mu.Unlock()
	if c == nil {
		return
	}
	raw, _ := json.Marshal(map[string]any{"type": typ, "data": data})
	_ = c.Write(f.t.Context(), websocket.MessageText, raw)
}

func (f *Fake) prompt(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	must(f.t, json.NewDecoder(r.Body).Decode(&body))
	var id, client string
	must(f.t, json.Unmarshal(body["prompt_id"], &id))
	must(f.t, json.Unmarshal(body["client_id"], &client))
	f.mu.Lock()
	f.submitted = append(f.submitted, body)
	hold := f.Hold
	f.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"prompt_id": id, "number": 1, "node_errors": map[string]any{}})
	go f.run(id, client, hold, body["prompt"])
}

// output is the node and file name the history lists: what the test set, else the graph's own Save node
// (SaveImage: out.png, SaveVideo: clip.mp4).
func (f *Fake) output(graph json.RawMessage) (node, file string) {
	f.mu.Lock()
	node, file = f.OutputNode, f.OutputFile
	f.mu.Unlock()
	var g map[string]struct {
		ClassType string `json:"class_type"`
	}
	if node != "" || json.Unmarshal(graph, &g) != nil {
		return node, file
	}
	for id, n := range g {
		switch n.ClassType {
		case "SaveImage":
			node, file = id, "out.png"
		case "SaveVideo":
			node, file = id, "clip.mp4"
		}
	}
	if f.OutputFile != "" {
		file = f.OutputFile
	}
	return node, file
}

// run plays the execution of one prompt.
func (f *Fake) run(id, client string, hold chan struct{}, graph json.RawMessage) {
	// Wait for the backend's socket (it is opened before the prompt, but the registration races).
	for range 200 {
		f.mu.Lock()
		_, ok := f.sockets[client]
		f.mu.Unlock()
		if ok || f.NoSocket {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.send(client, "execution_cached", map[string]any{"prompt_id": id, "nodes": []string{"4"}})
	f.send(client, "executing", map[string]any{"prompt_id": id, "node": "3"})
	f.send(client, "progress", map[string]any{"prompt_id": id, "node": "3", "value": 5, "max": 10})
	f.send(client, "progress", map[string]any{"prompt_id": "someone-elses", "node": "3", "value": 9, "max": 10})
	if hold != nil {
		<-hold
	}
	f.mu.Lock()
	FailWith := f.FailWith
	f.mu.Unlock()
	node, file := f.output(graph)
	if FailWith != "" {
		f.mu.Lock()
		f.history[id] = `{"outputs":{},"status":{"status_str":"error","completed":false,"messages":[["execution_error",{"node_type":"KSampler","exception_message":"` + FailWith + `"}]]}}`
		f.mu.Unlock()
		f.send(client, "execution_error", map[string]any{"prompt_id": id, "exception_message": FailWith})
		return
	}
	f.mu.Lock()
	f.history[id] = `{"outputs":{"` + node + `":{"images":[{"filename":"` + file + `","subfolder":"sub","type":"output"}]}},"status":{"status_str":"success","completed":true,"messages":[]}}`
	f.mu.Unlock()
	f.send(client, "executing", map[string]any{"prompt_id": id, "node": nil})
}
