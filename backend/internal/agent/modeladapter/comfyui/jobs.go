package comfyui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

var jsonHeader = map[string]string{"Content-Type": "application/json"}

// submit queues the prompt (POST /prompt) under the ID the client chose, so that the job can be cancelled
// even when the answer is lost. It returns the ID the server uses.
func (b *Backend) submit(ctx context.Context, graph json.RawMessage, clientID, promptID string) (string, error) {
	body, _ := json.Marshal(map[string]any{"prompt": graph, "client_id": clientID, "prompt_id": promptID})
	data, err := b.Client.Request(ctx, http.MethodPost, "/prompt", jsonHeader, body, localhttp.MaxJSONBytes)
	if err != nil {
		return promptID, err
	}
	var out struct {
		PromptID string `json:"prompt_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.PromptID == "" {
		return promptID, b.Client.Empty("prompt id")
	}
	return out.PromptID, nil
}

// history is one entry of GET /history/<id>.
type history struct {
	Outputs map[string]map[string]json.RawMessage `json:"outputs"`
	Status  struct {
		StatusStr string              `json:"status_str"`
		Completed bool                `json:"completed"`
		Messages  [][]json.RawMessage `json:"messages"`
	} `json:"status"`
}

// state asks the history for the prompt: done is false while it queues or runs (the entry is not there yet).
func (b *Backend) state(ctx context.Context, promptID string) (h history, done bool, err error) {
	err = b.Client.Repeat(ctx, func() error {
		data, err := b.Client.Request(ctx, http.MethodGet, "/history/"+url.PathEscape(promptID), nil, nil, localhttp.MaxJSONBytes)
		if err != nil {
			return err
		}
		var all map[string]history
		if err := json.Unmarshal(data, &all); err != nil {
			return b.Client.Error(http.StatusOK, "undecodable history", err)
		}
		h, done = all[promptID]
		return nil
	})
	if done && !h.Status.Completed && h.Status.StatusStr != "error" && h.Status.StatusStr != "success" {
		done = false // an entry that says nothing about its end yet
	}
	return h, done, err
}

// failure is why a finished prompt ended in error: the exception message of its execution_error event.
func (h history) failure() string {
	for _, m := range h.Status.Messages {
		if len(m) != 2 {
			continue
		}
		var kind string
		var detail struct {
			Message string `json:"exception_message"`
			Node    string `json:"node_type"`
		}
		if json.Unmarshal(m[0], &kind) == nil && kind == "execution_error" && json.Unmarshal(m[1], &detail) == nil {
			return strings.TrimSpace(detail.Node + ": " + detail.Message)
		}
	}
	return "the workflow failed"
}

// file is an output file as the history lists it.
type file struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

// outputs are the files of the node, in the order the history lists them (an image node lists them under
// "images", a video node too, with "animated": [true]; other keys are told apart by their shape).
func (h history) outputs(node string) []file {
	var files []file
	keys := make([]string, 0, len(h.Outputs[node]))
	for k := range h.Outputs[node] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		var list []file
		if json.Unmarshal(h.Outputs[node][k], &list) != nil {
			continue
		}
		for _, f := range list {
			if f.Filename != "" {
				files = append(files, f)
			}
		}
	}
	return files
}

// pick is the file to take: the first one the node saved to the output directory, else the first.
func pick(files []file) (file, bool) {
	if len(files) == 0 {
		return file{}, false
	}
	if i := slices.IndexFunc(files, func(f file) bool { return f.Type == "output" }); i >= 0 {
		return files[i], true
	}
	return files[0], true
}

// viewPath is the GET /view path of a file.
func viewPath(f file) string {
	q := url.Values{"filename": {f.Filename}, "subfolder": {f.Subfolder}, "type": {cmpType(f.Type)}}
	return "/view?" + q.Encode()
}

func cmpType(t string) string {
	if t == "" {
		return "output"
	}
	return t
}

// cancel stops the prompt, if it is still queued or running, without touching the jobs of other clients:
// POST /api/jobs/<id>/cancel where the server has it, else the queue is read and the prompt is interrupted
// (only when it is the running one) or deleted from the queue by its ID. It never sends an unscoped
// interrupt or a clear. It runs after the call's own context ended, so it has its own short deadline.
func (b *Backend) cancel(promptID string) {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_, err := b.Client.Request(ctx, http.MethodPost, "/api/jobs/"+url.PathEscape(promptID)+"/cancel", jsonHeader, []byte("{}"), localhttp.MaxJSONBytes)
	if err == nil || !isNotFound(err) {
		return
	}
	data, err := b.Client.Request(ctx, http.MethodGet, "/queue", nil, nil, localhttp.MaxJSONBytes)
	if err != nil {
		return
	}
	var q struct {
		Running [][]json.RawMessage `json:"queue_running"`
		Pending [][]json.RawMessage `json:"queue_pending"`
	}
	if json.Unmarshal(data, &q) != nil {
		return
	}
	switch {
	case inQueue(q.Running, promptID):
		body, _ := json.Marshal(map[string]string{"prompt_id": promptID})
		_, _ = b.Client.Request(ctx, http.MethodPost, "/interrupt", jsonHeader, body, localhttp.MaxJSONBytes)
	case inQueue(q.Pending, promptID):
		body, _ := json.Marshal(map[string][]string{"delete": {promptID}})
		_, _ = b.Client.Request(ctx, http.MethodPost, "/queue", jsonHeader, body, localhttp.MaxJSONBytes)
	}
}

// inQueue reports whether a queue entry ([number, prompt_id, ...]) is the prompt.
func inQueue(entries [][]json.RawMessage, promptID string) bool {
	for _, e := range entries {
		var id string
		if len(e) > 1 && json.Unmarshal(e[1], &id) == nil && id == promptID {
			return true
		}
	}
	return false
}
