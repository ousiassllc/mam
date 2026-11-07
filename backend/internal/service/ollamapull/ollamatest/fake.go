// Package ollamatest is an ollamapull.Service over fakes, for the tests of the layers above it (the screen's handlers, the MCP tools) and its own: a fake Ollama (an HTTP server that streams the NDJSON
// of /api/pull, lists /api/tags and answers /api/delete, and writes the manifest the way Ollama does), a fake runtime manager that starts and stops it, and a temporary directory next to the "exe". The models are
// the three of the real catalog. Not for production code.
package ollamatest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
)

// Line is one line of the NDJSON of /api/pull.
type Line map[string]any

// Status is a line of progress of a layer: the layer's own total and completed bytes.
func Status(status, digest string, total, completed int64) Line {
	return Line{"status": status, "digest": digest, "total": total, "completed": completed}
}

// Fake is the Ollama that the rig runs: it answers on Server.URL.
type Fake struct {
	*httptest.Server
	t       *testing.T
	modelsd string

	mu sync.Mutex
	// Lines are what /api/pull streams, in order; the line "success" ends it. Hold, when set, makes the stream wait before Hold.At lines are sent (a test cancels there).
	Lines []Line
	// Tags are the models /api/tags lists (name and size).
	Tags []Tag
	// Manifest is what the fake writes when a pull succeeds: the model layer of the pulled name (nil: the one of the real catalog, so the pull is "verified").
	Manifest func(name string) any
	// DeleteStatus is the status /api/delete answers (0: 200).
	DeleteStatus int
	// Models are the models the manifests are written for (nil: the real catalog's).
	Models   []ollamamodels.Model
	pulls    []string
	deletes  []string
	requests []string
	open     chan struct{} // closed when a pull is in the middle of its stream
	hold     chan struct{}
	holdAt   int
}

// Tag is a model of /api/tags.
type Tag struct {
	Name string
	Size int64
}

func newFake(t *testing.T, modelsDir string) *Fake {
	t.Helper()
	f := &Fake{t: t, modelsd: modelsDir, open: make(chan struct{}, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pull", f.pull)
	mux.HandleFunc("/api/tags", f.tags)
	mux.HandleFunc("/api/delete", f.delete)
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"version":"0.0.0-test"}`)) })
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

// Requests are the "METHOD path" of every request the fake got, in order.
func (f *Fake) Requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// Pulls are the models /api/pull was asked for; Deletes the ones /api/delete was.
func (f *Fake) Pulls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.pulls...)
}

// Deletes are the models /api/delete was asked for.
func (f *Fake) Deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletes...)
}

// HoldAt makes the next stream wait before it sends line number at (0-based) until release is called or the client closes the connection; Reached then says a pull waits there.
func (f *Fake) HoldAt(at int) (release func()) {
	f.mu.Lock()
	f.hold, f.holdAt = make(chan struct{}), at
	ch := f.hold
	f.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// Reached waits until a stream is held.
func (f *Fake) Reached() {
	f.t.Helper()
	select {
	case <-f.open:
	case <-time.After(20 * time.Second):
		f.t.Fatal("the pull never reached the fake")
	}
}

func (f *Fake) pull(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	f.pulls = append(f.pulls, in.Model)
	lines, hold, holdAt := append([]Line(nil), f.Lines...), f.hold, f.holdAt
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-ndjson")
	fl, _ := w.(http.Flusher)
	for i, l := range lines {
		if hold != nil && i == holdAt {
			f.open <- struct{}{}
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		if l["status"] == "success" {
			f.writeManifest(in.Model)
		}
		data, _ := json.Marshal(l)
		_, _ = w.Write(append(data, '\n'))
		if fl != nil {
			fl.Flush()
		}
	}
}

func (f *Fake) writeManifest(name string) {
	var body any
	f.mu.Lock()
	custom := f.Manifest
	f.mu.Unlock()
	if custom != nil {
		body = custom(name)
	} else {
		body = manifestFor(f.modelsOrCatalog(), name, "")
	}
	data, err := json.Marshal(body)
	require.NoError(f.t, err)
	if str, ok := body.(string); ok {
		data = []byte(str) // a manifest that is not JSON
	}
	repo, tag, _ := strings.Cut(name, ":")
	path := filepath.Join(f.modelsd, "manifests", "registry.ollama.ai", "library", repo, tag)
	require.NoError(f.t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(f.t, os.WriteFile(path, data, 0o600))
}

func (f *Fake) modelsOrCatalog() []ollamamodels.Model {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Models != nil {
		return f.Models
	}
	return catalog.Default().OllamaModels
}

// ManifestOf is the manifest of the real catalog's model name as the registry has it on the day it was recorded, with the layer of the weights having the digest digest ("": the recorded one).
func ManifestOf(name, digest string) map[string]any {
	return manifestFor(catalog.Default().OllamaModels, name, digest)
}

func manifestFor(models []ollamamodels.Model, name, digest string) map[string]any {
	m, ok := ollamamodels.Find(models, name)
	if !ok {
		return map[string]any{"layers": []any{}}
	}
	var layers []any
	for _, l := range m.Layers {
		d := l.Digest
		if l.Kind == ollamamodels.KindModel && digest != "" {
			d = digest
		}
		layers = append(layers, map[string]any{"mediaType": "application/vnd.ollama.image." + l.Kind, "digest": d, "size": l.Size})
	}
	return map[string]any{"schemaVersion": 2, "layers": layers}
}

func (f *Fake) tags(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	tags := append([]Tag(nil), f.Tags...)
	f.mu.Unlock()
	var models []map[string]any
	for _, t := range tags {
		models = append(models, map[string]any{"name": t.Name, "model": t.Name, "size": t.Size})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
}

func (f *Fake) delete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	f.deletes = append(f.deletes, in.Model)
	status := f.DeleteStatus
	f.mu.Unlock()
	if status != 0 {
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"error":"model %q not found"}`, in.Model)
		return
	}
	repo, tag, _ := strings.Cut(in.Model, ":")
	_ = os.Remove(filepath.Join(f.modelsd, "manifests", "registry.ollama.ai", "library", repo, tag))
	w.WriteHeader(http.StatusOK)
}
