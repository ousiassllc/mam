package topaz_test

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/topaz"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

const testKey = "test-key"

// request is what a fake server received.
type request struct {
	Method, Path, Key, ContentType string
	ContentLength                  int64
	Body                           []byte
}

// fake is a Topaz of two hosts: api (the Video API, which needs the key) and storage (the presigned upload and download URLs, which must never
// see it). Every handler can be replaced; the defaults are the happy path.
type fake struct {
	t       *testing.T
	api     *httptest.Server
	storage *httptest.Server

	mu       sync.Mutex
	requests []request // api and storage together, in order
	polls    int

	onCreate   func(w http.ResponseWriter, r *http.Request)
	onAccept   func(w http.ResponseWriter, r *http.Request)
	onComplete func(w http.ResponseWriter, r *http.Request)
	// onStatus answers the nth (from 1) status request.
	onStatus   func(w http.ResponseWriter, n int)
	onUpload   func(w http.ResponseWriter, r *http.Request)
	onDownload func(w http.ResponseWriter, r *http.Request)
	// uploadURL is the URL accept returns ("" is the upload path of storage).
	uploadURL string
	// downloadURL is the URL of a complete status ("" is the download path of storage).
	downloadURL string
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{t: t}
	f.onCreate = func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"requestId":"req-1","estimates":{"cost":[8,12]}}`)
	}
	f.onAccept = func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"urls":[%q]}`, cmp.Or(f.uploadURL, f.storage.URL+"/up")))
	}
	f.onComplete = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, `{"status":"queued"}`) }
	f.onStatus = func(w http.ResponseWriter, n int) {
		switch n {
		case 1:
			writeJSON(w, http.StatusOK, `{"status":"Accepted"}`)
		case 2:
			writeJSON(w, http.StatusOK, `{"status":"Processing","progress":40}`)
		default:
			writeJSON(w, http.StatusOK, fmt.Sprintf(`{"status":"Complete","download":{"url":%q}}`, cmp.Or(f.downloadURL, f.storage.URL+"/out")))
		}
	}
	f.onUpload = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"etag-1"`)
		w.WriteHeader(http.StatusOK)
	}
	f.onDownload = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("RESULT")) }

	f.api = httptest.NewServer(http.HandlerFunc(f.serveAPI))
	f.storage = httptest.NewServer(http.HandlerFunc(f.serveStorage))
	t.Cleanup(f.api.Close)
	t.Cleanup(f.storage.Close)
	return f
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (f *fake) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request{
		Method: r.Method, Path: r.URL.Path, Key: r.Header.Get("X-Api-Key"), ContentType: r.Header.Get("Content-Type"),
		ContentLength: r.ContentLength, Body: body,
	})
}

func (f *fake) serveAPI(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/video/":
		f.onCreate(w, r)
	case r.Method == http.MethodPatch && r.URL.Path == "/video/req-1/accept":
		f.onAccept(w, r)
	case r.Method == http.MethodPatch && r.URL.Path == "/video/req-1/complete-upload/":
		f.onComplete(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/video/req-1/status":
		f.mu.Lock()
		f.polls++
		n := f.polls
		f.mu.Unlock()
		f.onStatus(w, n)
	default:
		http.NotFound(w, r)
	}
}

func (f *fake) serveStorage(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	switch r.URL.Path {
	case "/up":
		f.onUpload(w, r)
	case "/out":
		f.onDownload(w, r)
	default:
		http.NotFound(w, r)
	}
}

// seen is the requests received so far whose "METHOD path" is call.
func (f *fake) seen(call string) []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []request
	for _, r := range f.requests {
		if r.Method+" "+r.Path == call {
			out = append(out, r)
		}
	}
	return out
}

func (f *fake) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// keyAt is the X-API-Key values the requests of one server carried (api or storage).
func (f *fake) keyAt(storage bool) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for _, r := range f.requests {
		isStorage := r.Path == "/up" || r.Path == "/out" || strings.HasPrefix(r.Path, "/redirected")
		if isStorage == storage {
			keys = append(keys, r.Key)
		}
	}
	return keys
}

// client is a Client of the fake with a stored key, transfers to the local servers allowed and a fast poll.
func (f *fake) client(mod ...func(*topaz.Config)) *topaz.Client {
	cfg := topaz.Config{BaseURL: f.api.URL, PollInterval: time.Millisecond, LocalTransfers: true}
	for _, m := range mod {
		m(&cfg)
	}
	return topaz.New(cfg, keyStore(f.t, testKey), nil)
}

func keyStore(t *testing.T, key string) *credentialtest.Memory {
	t.Helper()
	m := credentialtest.New()
	if key != "" {
		require.NoError(t, m.Set(topaz.CredentialProvider, key))
	}
	return m
}

// source is a 480 × 848, 24 fps, 97 frames mp4 with audio.
var source = cloudenhance.Source{Container: "mp4", SizeBytes: 12, Duration: 4, FrameCount: 97, FPS: 24, Width: 480, Height: 848, HasAudio: true}

func upscale(model string) cloudenhance.CloudRequest {
	return cloudenhance.CloudRequest{Model: model, Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 2}}
}

// sourceFile writes the file Run uploads.
func sourceFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.mp4")
	require.NoError(t, os.WriteFile(p, []byte("SOURCE-BYTES"), 0o600))
	return p
}

func runInput(t *testing.T) cloudenhance.RunInput {
	t.Helper()
	return cloudenhance.RunInput{RequestID: "req-1", SourcePath: sourceFile(t), DestPath: filepath.Join(t.TempDir(), "out.mp4")}
}

func background() context.Context { return context.Background() }
