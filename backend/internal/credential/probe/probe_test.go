package probe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/credential/probe"
)

const secret = "sk-test-0123456789"

// seen is what a fake provider received.
type seen struct {
	mu      sync.Mutex
	method  string
	path    string
	headers http.Header
}

func fake(t *testing.T, status int) (*httptest.Server, *seen) {
	t.Helper()
	got := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.mu.Lock()
		got.method, got.path, got.headers = r.Method, r.URL.RequestURI(), r.Header.Clone()
		got.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"echo":"` + r.Header.Get("X-Api-Key") + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// Each provider is called with the read-only request of its own reference page and the key in its own header, never in
// the URL.
func TestProber_CallsTheFreeReadOnlyEndpointWithTheKeyInTheProvidersHeader(t *testing.T) {
	tests := []struct {
		provider, path, header, value string
	}{
		{"anthropic", "/v1/models?limit=1", "X-Api-Key", secret},
		{"openai", "/v1/models", "Authorization", "Bearer " + secret},
		{"google", "/v1beta/models?pageSize=1", "X-Goog-Api-Key", secret},
		{"flux", "/v1/credits", "X-Key", secret},
		{"runway", "/v1/organization", "Authorization", "Bearer " + secret},
		{"elevenlabs", "/v1/user/subscription", "Xi-Api-Key", secret},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			srv, got := fake(t, http.StatusOK)

			res := probe.Prober{BaseURLs: map[string]string{tc.provider: srv.URL}}.Test(context.Background(), tc.provider, secret)

			assert.Equal(t, probe.Result{Outcome: probe.Accepted, Status: 200}, res)
			assert.Equal(t, http.MethodGet, got.method)
			assert.Equal(t, tc.path, got.path)
			assert.Equal(t, tc.value, got.headers.Get(tc.header))
			assert.NotContains(t, got.path, secret, "the key is never in the URL")
		})
	}
}

// The two providers that want a version header get the one their adapters send.
func TestProber_SendsTheVersionHeaderThatAnthropicAndRunwayAskFor(t *testing.T) {
	for provider, header := range map[string][2]string{"anthropic": {"Anthropic-Version", "2023-06-01"}, "runway": {"X-Runway-Version", "2024-11-06"}} {
		srv, got := fake(t, http.StatusOK)
		probe.Prober{BaseURLs: map[string]string{provider: srv.URL}}.Test(context.Background(), provider, secret)
		assert.Equal(t, header[1], got.headers.Get(header[0]), provider)
	}
}

func TestProber_TheStatusTellsWhatTheProviderSaid(t *testing.T) {
	tests := []struct {
		provider string
		status   int
		want     probe.Outcome
	}{
		{"openai", http.StatusUnauthorized, probe.Rejected},
		{"openai", http.StatusForbidden, probe.Rejected},
		{"openai", http.StatusBadRequest, probe.Unexpected},
		{"google", http.StatusBadRequest, probe.Rejected}, // Google answers 400 to a key it does not know
		{"openai", http.StatusTooManyRequests, probe.RateLimited},
		{"openai", http.StatusInternalServerError, probe.Unreachable},
		{"openai", http.StatusNotFound, probe.Unexpected},
	}
	for _, tc := range tests {
		srv, _ := fake(t, tc.status)
		res := probe.Prober{BaseURLs: map[string]string{tc.provider: srv.URL}}.Test(context.Background(), tc.provider, secret)
		assert.Equal(t, tc.want, res.Outcome, "%s %d", tc.provider, tc.status)
		assert.Equal(t, tc.status, res.Status)
	}
}

func TestProber_NoAnswerIsUnreachableAndAnUnsendableKeyIsRejectedWithoutARequest(t *testing.T) {
	srv, got := fake(t, http.StatusOK)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	assert.Equal(t, probe.Unreachable, probe.Prober{BaseURLs: map[string]string{"openai": closed.URL}}.Test(context.Background(), "openai", secret).Outcome)

	p := probe.Prober{BaseURLs: map[string]string{"openai": srv.URL}}
	for _, bad := range []string{"", "two words", "line\nbreak", "日本語"} {
		assert.Equal(t, probe.Rejected, p.Test(context.Background(), "openai", bad).Outcome, "%q", bad)
	}
	assert.Empty(t, got.method, "nothing was sent")
	assert.Equal(t, probe.Rejected, p.Test(context.Background(), "kling", secret).Outcome, "a provider without a test")
}

// A redirect to another host would carry the key header with it: it is refused, so the answer is "unreachable".
func TestProber_DoesNotFollowARedirectToAnotherHost(t *testing.T) {
	other, got := fake(t, http.StatusOK)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+"/stolen", http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	res := probe.Prober{BaseURLs: map[string]string{"flux": redirector.URL}}.Test(context.Background(), "flux", secret)

	assert.Equal(t, probe.Unreachable, res.Outcome)
	assert.Empty(t, got.method, "the other host never saw the key")
}

func TestSupported_OnlyTheProvidersWithAFreeReadOnlyCall(t *testing.T) {
	for _, p := range []string{"anthropic", "openai", "google", "flux", "runway", "elevenlabs"} {
		assert.True(t, probe.Supported(p), p)
	}
	for _, p := range []string{"kling", "seedance", "synclabs", "typesafe", ""} {
		assert.False(t, probe.Supported(p), p)
	}
}

// Every test uses the same client, so the connection of the first is kept for the next: a client for each test would
// leave a connection (and its goroutines) behind every click on "接続テスト".
func TestProber_TestsShareOneClientAndSoOneConnection(t *testing.T) {
	var mu sync.Mutex
	conns := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		conns[r.RemoteAddr] = true
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	p := probe.Prober{BaseURLs: map[string]string{"openai": srv.URL}}

	for range 3 {
		assert.Equal(t, probe.Accepted, p.Test(context.Background(), "openai", secret).Outcome)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, conns, 1, "the three calls went over one connection")
}
