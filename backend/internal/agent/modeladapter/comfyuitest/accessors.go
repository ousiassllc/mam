package comfyuitest

import (
	"encoding/json"
	"strings"
	"testing"
)

// Graph returns the nodes of the last submitted prompt.
func (f *Fake) Graph() map[string]struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submitted) == 0 {
		f.t.Fatal("no prompt was submitted")
	}
	var g map[string]struct {
		ClassType string                     `json:"class_type"`
		Inputs    map[string]json.RawMessage `json:"inputs"`
	}
	must(f.t, json.Unmarshal(f.submitted[len(f.submitted)-1]["prompt"], &g))
	return g
}

// Count is the number of requests whose "METHOD path" starts with prefix.
func (f *Fake) Count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Submitted is the body of every POST /prompt so far.
func (f *Fake) Submitted() []map[string]json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]json.RawMessage(nil), f.submitted...)
}

// PromptID is the prompt ID of the i-th submitted prompt.
func (f *Fake) PromptID(i int) string {
	var id string
	must(f.t, json.Unmarshal(f.Submitted()[i]["prompt_id"], &id))
	return id
}

// Uploads are the names of the files uploaded to /upload/image.
func (f *Fake) Uploads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.uploads...)
}

// Auths are the Authorization headers of the requests, in order, with their "METHOD path".
func (f *Fake) Auths() (requests, auths []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...), append([]string(nil), f.auths...)
}

// Cancelled are the job IDs of POST /api/jobs/<id>/cancel.
func (f *Fake) Cancelled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancelled...)
}

// Interrupts are the bodies of POST /interrupt.
func (f *Fake) Interrupts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.interrupt...)
}

// QueueDeletes are the bodies of POST /queue.
func (f *Fake) QueueDeletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queueDel...)
}

// Lock guards the exported fields while a test changes them.
func (f *Fake) Lock() { f.mu.Lock() }

// Unlock ends Lock.
func (f *Fake) Unlock() { f.mu.Unlock() }
