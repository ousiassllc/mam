// Package localhttp is the HTTP side of the backends of registered endpoints (ComfyUI and OpenAI-compatible
// servers; spec/architecture/local-models.md): requests to the endpoint, the optional API key, and the
// download of result files. It is what modeladapter's own vendor client is for the built-in vendors, with
// the rules of a registered endpoint: plain http and local addresses are fine for the endpoint's own origin
// (safefetch.OriginClient), the API key goes only under the endpoint's base URL, and a file URL of another
// origin is held to the rules for vendor result URLs.
package localhttp

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// Limits on what an endpoint may send back.
const (
	// MaxJSONBytes bounds a JSON answer (an image may come back base64-encoded).
	MaxJSONBytes = 64 << 20
	// MaxFileBytes bounds a downloaded image or video.
	MaxFileBytes = 300 << 20
)

// Client talks to one registered endpoint.
type Client struct {
	// Name is the endpoint's name; the vendor of its errors is localcfg.VendorKey(Name).
	Name string
	// Base is the endpoint's base URL without a trailing slash.
	Base string
	// Keys holds the endpoint's optional API key under localcfg.VendorKey(Name). Nil: no key.
	Keys credential.Store
	// Requests carries the requests to the endpoint and Sockets its WebSocket (both safefetch.NewEndpointClient);
	// Files the downloads (safefetch.OriginClient).
	Requests httpclient.Doer
	Sockets  *http.Client
	Files    httpclient.Doer
	// PollEvery is the wait between two status requests of a running job (0: 2 seconds).
	PollEvery time.Duration
	// StagingDir is where downloaded videos are staged ("" selects the system temporary directory).
	StagingDir string
}

// New returns the Client of an endpoint with the clients registered endpoints use.
func New(name string, e localcfg.Endpoint, keys credential.Store, stagingDir string) (*Client, error) {
	files, err := safefetch.NewOriginClient(e.BaseURL)
	if err != nil {
		return nil, err
	}
	requests := safefetch.NewEndpointClient()
	return &Client{
		Name: name, Base: strings.TrimRight(e.BaseURL, "/"), Keys: keys, Requests: requests, Sockets: requests,
		Files: files, StagingDir: stagingDir,
	}, nil
}

// Vendor is the name of the endpoint as a vendor of the Model Adapter (breaker, concurrency limit, errors).
func (c *Client) Vendor() string { return localcfg.VendorKey(c.Name) }

// Poll is PollEvery with the default applied.
func (c *Client) Poll() time.Duration { return cmp.Or(c.PollEvery, 2*time.Second) }

// Key is the API key of the endpoint, "" when none is stored.
func (c *Client) Key() (string, error) {
	if c.Keys == nil {
		return "", nil
	}
	key, err := c.Keys.Get(c.Vendor())
	if errors.Is(err, credential.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the API key of %s: %w", c.Name, err)
	}
	return key, nil
}

// Auth is the headers that authenticate a request to target: the bearer key when the endpoint has one and
// target is under its base URL, nothing otherwise (the key never leaves the endpoint).
func (c *Client) Auth(target string) (map[string]string, error) {
	h := map[string]string{}
	if !safefetch.Within(target, c.Base) {
		return h, nil
	}
	key, err := c.Key()
	if err != nil || key == "" {
		return h, err
	}
	h["Authorization"] = "Bearer " + key
	return h, nil
}

// Error is a ProviderError of this endpoint.
func (c *Client) Error(status int, message string, cause error) *modeladapter.ProviderError {
	return &modeladapter.ProviderError{Vendor: c.Vendor(), Status: status, Message: message, Err: cause}
}

// Empty is the error of an answer that lacks what the call needs.
func (c *Client) Empty(what string) error {
	return c.Error(http.StatusOK, "the answer has no "+what, nil)
}

// Failed is the error of a job the endpoint finished without a result (an invalid workflow, a failed node).
func (c *Client) Failed(reason string) error {
	return c.Error(http.StatusUnprocessableEntity, reason, nil)
}

// Request sends a request to path of the endpoint and returns the body of a 2xx answer.
func (c *Client) Request(ctx context.Context, method, path string, header map[string]string, body []byte, limit int64) ([]byte, error) {
	target := c.Base + path
	auth, err := c.Auth(target)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", c.Name, err)
	}
	for _, h := range []map[string]string{auth, header} {
		for k, v := range h {
			req.Header.Set(k, v)
		}
	}
	res, err := c.Requests.Do(req)
	if err != nil {
		return nil, c.transportError(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, c.Error(0, "", err)
	}
	if int64(len(data)) > limit {
		return nil, c.Error(http.StatusOK, "the answer is too large", nil)
	}
	if res.StatusCode/100 != 2 {
		pe := c.Error(res.StatusCode, httpclient.Snippet(data), nil)
		pe.RetryAfter = httpclient.ParseRetryAfter(res.Header.Get("Retry-After"))
		return nil, pe
	}
	return data, nil
}

// transportError is the ProviderError of a request that got no answer. An address or URL the policy refuses
// is not a network failure: it is a 422 (the request itself is unusable, which no retry or breaker counts as
// unavailable) that still wraps safefetch.ErrRefused.
func (c *Client) transportError(err error) error {
	if errors.Is(err, safefetch.ErrRefused) {
		return c.Error(http.StatusUnprocessableEntity, httpclient.RedactURL(err).Error(), err)
	}
	return c.Error(0, "", httpclient.RedactURL(err))
}

// Repeat runs fn again while it fails with an unavailable answer, until ctx ends: the job is running at the
// endpoint already, so the deadline of the call, not a number of tries, bounds it. fn must be repeatable.
func (c *Client) Repeat(ctx context.Context, fn func() error) error {
	retryAfter := func(err error) time.Duration {
		var pe *modeladapter.ProviderError
		if errors.As(err, &pe) {
			return pe.RetryAfter
		}
		return 0
	}
	return resilience.Repeat(ctx, resilience.Backoff{Base: c.Poll(), Max: resilience.JobRetryMaxDelay},
		resilience.Hooks{Unavailable: modeladapter.IsUnavailable, RetryAfter: retryAfter}, fn)
}

// Download fetches the file at fileURL (an absolute URL: an answer of the endpoint names it) and returns it
// with its Content-Type. With stage the body is streamed to a file of the staging directory (a video);
// otherwise it is kept in memory (an image). It only runs after the job was accepted, so a transient failure
// is repeated, and any error it finally returns is marked modeladapter.AfterSubmit.
func (c *Client) Download(ctx context.Context, fileURL string, stage bool, limit int64) (modeladapter.Blob, string, error) {
	var (
		blob modeladapter.Blob
		mime string
	)
	err := c.Repeat(ctx, func() (err error) {
		blob, mime, err = c.downloadOnce(ctx, fileURL, stage, limit)
		return err
	})
	if err != nil {
		return modeladapter.Blob{}, "", modeladapter.AfterSubmit(err)
	}
	return blob, mime, nil
}

func (c *Client) downloadOnce(ctx context.Context, fileURL string, stage bool, limit int64) (modeladapter.Blob, string, error) {
	auth, err := c.Auth(fileURL)
	if err != nil {
		return modeladapter.Blob{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return modeladapter.Blob{}, "", fmt.Errorf("build %s download: %w", c.Name, err)
	}
	for k, v := range auth {
		req.Header.Set(k, v)
	}
	res, err := c.Files.Do(req)
	if err != nil {
		return modeladapter.Blob{}, "", c.transportError(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		pe := c.Error(res.StatusCode, "download of the result failed", nil)
		pe.RetryAfter = httpclient.ParseRetryAfter(res.Header.Get("Retry-After"))
		return modeladapter.Blob{}, "", pe
	}
	tooLarge := c.Error(http.StatusOK, "the result file is too large", nil)
	if res.ContentLength > limit {
		return modeladapter.Blob{}, "", tooLarge
	}
	var blob modeladapter.Blob
	if stage {
		var path string
		var n int64
		path, n, err = fsutil.StageReader(c.StagingDir, res.Body, limit)
		blob = modeladapter.StagedBlob(path, n)
	} else {
		var data []byte
		if data, err = io.ReadAll(io.LimitReader(res.Body, limit+1)); err == nil && int64(len(data)) > limit {
			err = fsutil.ErrTooLarge
		}
		blob = modeladapter.BlobOf(data)
	}
	var local *fsutil.LocalError
	switch {
	case errors.Is(err, fsutil.ErrTooLarge):
		return modeladapter.Blob{}, "", tooLarge
	case errors.As(err, &local):
		return modeladapter.Blob{}, "", err // the staging disk failed, not the endpoint
	case err != nil:
		return modeladapter.Blob{}, "", c.Error(0, "", err)
	}
	return blob, res.Header.Get("Content-Type"), nil
}
