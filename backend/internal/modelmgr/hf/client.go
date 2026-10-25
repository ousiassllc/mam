package hf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// DefaultBase is the Hugging Face site; the API is below /api.
const DefaultBase = "https://huggingface.co"

const (
	// maxBody bounds a JSON answer (a tree page of 1000 files is well under 1 MiB).
	maxBody = 32 << 20
	// maxRetries is how often a rate-limited (429) request is repeated.
	maxRetries = 3
	// maxWait is the longest a rate-limit answer is waited out: a longer hint (the anonymous window is minutes) is returned as an error.
	maxWait = 20 * time.Second
)

// Options of New. The zero value is the production setting.
type Options struct {
	// Base is the site (default DefaultBase). A test points it at a fake server.
	Base string
	// HTTP sends the requests (default: the client of fetch.NewClient, which follows redirects that pass CheckURL and drops the token when one leaves the host).
	HTTP httpclient.Doer
	// CheckURL accepts the URL of a request and of every redirect (default fetch.AllowedURL).
	CheckURL func(*url.URL) error
	// Token returns the token ("" for none). It is sent to AuthHost only.
	Token func() string
	// AuthHost is the one host that gets the token (default "huggingface.co").
	AuthHost string
	// Wait returns how long to wait before the attempt-th repeat of a rate-limited request, given the server's hint
	// (default: the hint, at least a second). A test returns 0.
	Wait func(attempt int, hint time.Duration) time.Duration
}

// Client reads Hugging Face. Safe for concurrent use.
type Client struct {
	base     string
	http     httpclient.Doer
	check    func(*url.URL) error
	token    func() string
	authHost string
	wait     func(int, time.Duration) time.Duration
}

// New returns a Client.
func New(o Options) *Client {
	c := &Client{base: strings.TrimSuffix(o.Base, "/"), http: o.HTTP, check: o.CheckURL, token: o.Token, authHost: o.AuthHost, wait: o.Wait}
	if c.base == "" {
		c.base = DefaultBase
	}
	if c.check == nil {
		c.check = fetch.AllowedURL
	}
	if c.authHost == "" {
		c.authHost = "huggingface.co"
	}
	if c.http == nil {
		c.http = fetch.NewClient(c.check, c.authHost)
	}
	if c.wait == nil {
		c.wait = defaultWait
	}
	return c
}

// WithToken returns a copy of c that sends the token tok() (c is not changed).
func (c *Client) WithToken(tok func() string) *Client {
	n := *c
	n.token = tok
	return &n
}

func defaultWait(_ int, hint time.Duration) time.Duration { return max(hint, time.Second) }

// HasToken reports whether a token is set.
func (c *Client) HasToken() bool { return c.token != nil && c.token() != "" }

// APIError is an answer that is not a success.
type APIError struct {
	Status int
	// Code is the x-error-code header ("GatedRepo", "RepoNotFound", "EntryNotFound", ...).
	Code string
	// Message is the "error" of the JSON body, if any.
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("hf: the server answered %d %s %s", e.Status, e.Code, e.Message)
}

// RateLimitError is a 429 that was still there after the repeats (or whose wait is too long to sit out).
type RateLimitError struct {
	// RetryAfter is when the window ends (0: unknown).
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "hf: rate limited" }

// request is one call of the API.
type request struct {
	method string
	path   string
	query  url.Values
	body   any
	header http.Header
}

// apiPath is "/api/models/<owner>/<name>/<rest...>" with every segment escaped.
func apiPath(repo string, rest ...string) string {
	p := "/api/models/" + escapeSegments(repo)
	for _, s := range rest {
		p += "/" + url.PathEscape(s)
	}
	return p
}

func escapeSegments(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// do sends r, repeating a 429 up to maxRetries times. The caller closes the body of a successful answer.
func (c *Client) do(ctx context.Context, r request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		res, err := c.once(ctx, r)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusTooManyRequests {
			return res, nil
		}
		hint := rateHint(res.Header)
		_ = res.Body.Close()
		if attempt >= maxRetries || hint > maxWait {
			return nil, &RateLimitError{RetryAfter: hint}
		}
		if err := sleep(ctx, c.wait(attempt, hint)); err != nil {
			return nil, err
		}
	}
}

func (c *Client) once(ctx context.Context, r request) (*http.Response, error) {
	u, err := url.Parse(c.base + r.path)
	if err != nil {
		return nil, err
	}
	if len(r.query) > 0 {
		u.RawQuery = r.query.Encode()
	}
	if err := c.check(u); err != nil {
		return nil, err
	}
	var body io.Reader
	if r.body != nil {
		raw, err := json.Marshal(r.body)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u.String(), body)
	if err != nil {
		return nil, err
	}
	for k, v := range r.header {
		req.Header[k] = v
	}
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != nil && strings.EqualFold(u.Hostname(), c.authHost) {
		if tok := c.token(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, httpclient.RedactURL(err)
	}
	return res, nil
}

// rateHint is how long the server says to wait: Retry-After, else the t= of the RateLimit header (seconds to the end of the window).
func rateHint(h http.Header) time.Duration {
	if d := httpclient.ParseRetryAfter(h.Get("Retry-After")); d > 0 {
		return d
	}
	for _, part := range strings.Split(h.Get("RateLimit"), ";") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(part), "t="); ok {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-t.C:
		return nil
	}
}

// getJSON sends r and decodes a 200 answer into v; any other status is an *APIError.
func (c *Client) getJSON(ctx context.Context, r request, v any) (http.Header, error) {
	res, err := c.do(ctx, r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, apiError(res, raw)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return nil, fmt.Errorf("hf: the answer is not the expected JSON: %w", err)
	}
	return res.Header, nil
}

func apiError(res *http.Response, raw []byte) *APIError {
	e := &APIError{Status: res.StatusCode, Code: res.Header.Get("X-Error-Code")}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil {
		e.Message = httpclient.Snippet([]byte(body.Error))
	}
	return e
}

// IsStatus reports whether err is an *APIError with one of the statuses.
func IsStatus(err error, statuses ...int) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	for _, s := range statuses {
		if ae.Status == s {
			return true
		}
	}
	return false
}

// ResolveURL is the pinned download URL of a file: <base>/<repo>/resolve/<commit>/<path>.
func (c *Client) ResolveURL(repo, commit, path string) string {
	return c.base + "/" + escapeSegments(repo) + "/resolve/" + url.PathEscape(commit) + "/" + escapeSegments(path)
}

// PageURL is the page of a repository (for the user to open: license, access request).
func (c *Client) PageURL(repo string) string { return c.base + "/" + escapeSegments(repo) }
