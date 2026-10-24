package oauth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
)

// request is one call the fake authorization server saw.
type request struct {
	Path        string
	ContentType string
	Form        url.Values
	JSON        map[string]any
}

// fakeServer is a stand-in for auth.openai.com. handler decides the answer; every request is recorded.
type fakeServer struct {
	*httptest.Server
	mu   sync.Mutex
	seen []request
}

func newFakeServer(t *testing.T, handler func(w http.ResponseWriter, r request)) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := request{Path: r.URL.Path, ContentType: r.Header.Get("Content-Type")}
		switch {
		case strings.HasPrefix(req.ContentType, "application/json"):
			_ = json.Unmarshal(body, &req.JSON)
		case strings.HasPrefix(req.ContentType, "application/x-www-form-urlencoded"):
			req.Form, _ = url.ParseQuery(string(body))
		}
		f.mu.Lock()
		f.seen = append(f.seen, req)
		f.mu.Unlock()
		handler(w, req)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) requests(path string) []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []request
	for _, r := range f.seen {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fixedNow is the clock of the tests.
var fixedNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func testClient(t *testing.T) (*oauth.Client, *[]time.Duration) {
	t.Helper()
	c := oauth.NewClient(nil)
	now := fixedNow
	c.Now = func() time.Time { return now }
	slept := &[]time.Duration{}
	c.Sleep = func(_ context.Context, d time.Duration) error {
		*slept = append(*slept, d)
		now = now.Add(d) // time passes only while the client waits
		return nil
	}
	return c, slept
}
