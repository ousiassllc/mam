// Package probe tries an API key against the provider that issued it, with a call that is free and only reads, so that
// the settings screen can say "this key is accepted" before the first paid generation (spec/components/
// settings-screen-categories.md §1.1). Only the providers whose documentation lists such a call are here; for the others
// Supported is false and the screen says that the first generation is the test.
//
// The calls are the ones of the providers' own reference pages (checked 2026-10-25, not run with a real key):
// Anthropic GET /v1/models, OpenAI GET /v1/models, Google GET /v1beta/models, Black Forest Labs GET /v1/credits,
// Runway GET /v1/organization and ElevenLabs GET /v1/user/subscription. A key is sent in the header the provider's
// adapter uses and nowhere else: never in a URL, never logged, never part of a result.
package probe

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// Timeout bounds one test: the screen waits for it.
const Timeout = 10 * time.Second

// Versions the providers ask for in a header: the same as their adapters send.
const (
	anthropicVersion = "2023-06-01"
	runwayVersion    = "2024-11-06"
)

// maxBody is how much of an answer is read (and thrown away): the status is the whole answer.
const maxBody = 64 << 10

// Outcome is what the provider said to the key.
type Outcome string

// The outcomes. Accepted: the call worked with the key. Rejected: the provider refused the key (401 / 403; Google also
// 400 for a key it does not know). A key limited to other scopes than the read call needs is refused the same way, so the
// screen does not say more than "refused, or lacking the permission of this check". RateLimited: 429. Unreachable: no answer (network, DNS, timeout) or a server error.
// Unexpected: any other status; the key is neither accepted nor refused.
const (
	Accepted    Outcome = "accepted"
	Rejected    Outcome = "rejected"
	RateLimited Outcome = "rate_limited"
	Unreachable Outcome = "unreachable"
	Unexpected  Outcome = "unexpected"
)

// Result is the outcome and the HTTP status behind it (0 when there was none).
type Result struct {
	Outcome Outcome
	Status  int
}

type call struct {
	baseURL string
	path    string
	headers func(key string) map[string]string
	// rejectedBadRequest: the provider answers 400, not 401, to a key it does not know.
	rejectedBadRequest bool
}

func bearer(key string) map[string]string { return map[string]string{"Authorization": "Bearer " + key} }

var calls = map[string]call{
	"anthropic": {baseURL: "https://api.anthropic.com", path: "/v1/models?limit=1", headers: func(k string) map[string]string {
		return map[string]string{"x-api-key": k, "anthropic-version": anthropicVersion}
	}},
	"openai": {baseURL: "https://api.openai.com", path: "/v1/models", headers: bearer},
	"google": {baseURL: "https://generativelanguage.googleapis.com", path: "/v1beta/models?pageSize=1", rejectedBadRequest: true,
		headers: func(k string) map[string]string { return map[string]string{"x-goog-api-key": k} }},
	"flux": {baseURL: "https://api.bfl.ai", path: "/v1/credits", headers: func(k string) map[string]string {
		return map[string]string{"x-key": k}
	}},
	"runway": {baseURL: "https://api.dev.runwayml.com", path: "/v1/organization", headers: func(k string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + k, "X-Runway-Version": runwayVersion}
	}},
	"elevenlabs": {baseURL: "https://api.elevenlabs.io", path: "/v1/user/subscription", headers: func(k string) map[string]string {
		return map[string]string{"xi-api-key": k}
	}},
}

// Supported reports whether the provider has a test.
func Supported(provider string) bool {
	_, ok := calls[provider]
	return ok
}

// sharedClient is the client of every Prober without one: a client keeps its connections, so one for each test would
// leave a kept-alive connection behind every click.
var sharedClient = sync.OnceValue(httpclient.New)

// Prober makes the calls. The zero value works: the providers' own hosts and a client that does not follow a redirect to
// another host (the key header would go with it).
type Prober struct {
	// Client sends the requests; nil is one httpclient.New() client shared by all (not one for each test).
	Client httpclient.Doer
	// BaseURLs replaces the host of a provider (tests); a provider that is not in it keeps its own.
	BaseURLs map[string]string
}

// Test calls the provider with key. It does not return the key and does not keep it. The caller has cleaned the key
// (credential.CleanAPIKey); one that could not be sent as a header value is Rejected without a request. Context bounds
// the call; Timeout is applied on top of it.
func (p Prober) Test(ctx context.Context, provider, key string) Result {
	c, ok := calls[provider]
	if !ok || !headerSafe(key) {
		return Result{Outcome: Rejected}
	}
	base := c.baseURL
	if u := p.BaseURLs[provider]; u != "" {
		base = u
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+c.path, http.NoBody)
	if err != nil {
		return Result{Outcome: Unreachable}
	}
	for name, value := range c.headers(key) {
		req.Header.Set(name, value)
	}
	req.Header.Set("Accept", "application/json")
	client := p.Client
	if client == nil {
		client = sharedClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{Outcome: Unreachable}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	return Result{Outcome: outcomeOf(c, resp.StatusCode), Status: resp.StatusCode}
}

func outcomeOf(c call, status int) Outcome {
	switch {
	case status >= 200 && status < 300:
		return Accepted
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return Rejected
	case status == http.StatusBadRequest && c.rejectedBadRequest:
		return Rejected
	case status == http.StatusTooManyRequests:
		return RateLimited
	case status >= 500:
		return Unreachable
	}
	return Unexpected
}

// headerSafe is whether key can be a header value: visible ASCII only (a key with a space or a control character is
// not one the provider issued).
func headerSafe(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}
