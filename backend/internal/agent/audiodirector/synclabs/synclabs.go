// Package synclabs is the Sync Labs Lip Sync adapter of the Audio Director (POST /v2/generate, then poll GET /v2/generate/{id}); it
// implements audiodirector.LipSyncer.
package synclabs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// Config is the Sync Labs setting.
type Config struct {
	BaseURL string
	// Timeout bounds one Lip Sync from upload to download.
	Timeout time.Duration
	// PollInterval is the pause between two status requests.
	PollInterval time.Duration
	// LocalDownloads lets the output be downloaded from a plain http URL or a local address. It
	// exists for tests against httptest servers; by default the output URL must be https and
	// the download only connects to public addresses.
	LocalDownloads bool
}

// DefaultConfig is the setting used unless the caller changes it.
func DefaultConfig() Config {
	return Config{BaseURL: "https://api.sync.so", Timeout: 10 * time.Minute, PollInterval: 2 * time.Second}
}

// maxLipSyncBytes bounds a lip-synced video.
const maxLipSyncBytes = 512 << 20

// Client is the Sync Labs Lip Sync adapter.
type Client struct {
	cfg    Config
	keys   credential.Store
	client httpclient.Doer
	// fetcher downloads the output, whose URL comes from the vendor's answer.
	fetcher httpclient.Doer
}

// New returns a Client. A nil client selects a plain http.Client for the API and
// a client that connects to public addresses only (safefetch.NewClient) for the download; a given
// client serves both. Zero fields of cfg take the DefaultConfig values.
func New(cfg Config, keys credential.Store, client httpclient.Doer) *Client {
	def := DefaultConfig()
	if cfg.BaseURL == "" {
		cfg.BaseURL = def.BaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = def.PollInterval
	}
	fetcher := client
	if client == nil {
		client = httpclient.New()
		fetcher = client
		if !cfg.LocalDownloads {
			fetcher = safefetch.NewClient()
		}
	}
	return &Client{cfg: cfg, keys: keys, client: client, fetcher: fetcher}
}

// generation is the part of the Sync Labs generation object the adapter reads.
type generation struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	OutputURL string `json:"outputUrl"`
}

// Sync implements LipSyncer.
func (s *Client) Sync(ctx context.Context, req audiodirector.LipSyncRequest) ([]byte, error) {
	key, err := s.keys.Get(audiodirector.SyncCredentialProvider)
	if errors.Is(err, credential.ErrNotFound) {
		return nil, audiodirector.ErrNoSyncLabsKey
	}
	if err != nil {
		return nil, fmt.Errorf("read Sync Labs API key: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	fail := func(err error) error {
		var pe *audiodirector.ProviderError
		if errors.As(err, &pe) || errors.Is(err, context.Canceled) {
			return err
		}
		return &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Err: audiodirector.Redact(err, key)}
	}

	gen, err := s.create(ctx, key, req)
	if err != nil {
		return nil, fail(err)
	}
	for gen.Status != "COMPLETED" {
		switch gen.Status {
		case "FAILED", "REJECTED":
			return nil, &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Message: gen.Status + ": " + gen.Error}
		}
		select {
		case <-ctx.Done():
			return nil, &audiodirector.ChargedError{Err: fail(ctx.Err())}
		case <-time.After(s.cfg.PollInterval):
		}
		id := gen.ID
		if err = s.repeat(ctx, func() (err error) { gen, err = s.status(ctx, key, id); return err }); err != nil {
			return nil, &audiodirector.ChargedError{Err: fail(err)}
		}
	}
	if gen.OutputURL == "" {
		return nil, &audiodirector.ChargedError{Err: &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Message: "the generation has no outputUrl"}}
	}
	var video []byte
	if err = s.repeat(ctx, func() (err error) { video, err = s.download(ctx, gen.OutputURL); return err }); err != nil {
		return nil, &audiodirector.ChargedError{Err: fail(err)}
	}
	return video, nil
}

// repeat runs fn again while it fails with a transient error (429, 5xx, a broken connection), for as
// long as ctx lives: the Sync's Timeout, not a number of tries, bounds it, because the job was
// accepted (and is billed) already and its output is lost when a vendor outage of some seconds ends
// the wait. It waits PollInterval, doubling up to resilience.JobRetryMaxDelay, or the Retry-After of
// the answer when that is longer. fn must be safe to repeat: a GET of a job that was already accepted.
func (s *Client) repeat(ctx context.Context, fn func() error) error {
	return resilience.Repeat(ctx, resilience.Backoff{Base: s.cfg.PollInterval, Max: resilience.JobRetryMaxDelay},
		resilience.Hooks{Unavailable: transient}, fn)
}

// retryAfter attaches the Retry-After of a failed answer to err, so that repeat waits for it.
func retryAfter(res *http.Response, err error) error {
	return resilience.WithRetryAfter(err, httpclient.ParseRetryAfter(res.Header.Get("Retry-After")))
}

// transient reports whether err is a 429, a 5xx or a network failure (a retry can succeed).
func transient(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var pe *audiodirector.ProviderError
	if errors.As(err, &pe) {
		return pe.Status == http.StatusTooManyRequests || pe.Status >= 500 || (pe.Status == 0 && pe.Err != nil)
	}
	return true // the HTTP client could not complete the request
}

func (s *Client) create(ctx context.Context, key string, req audiodirector.LipSyncRequest) (generation, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("model", req.Model); err != nil {
		return generation{}, err
	}
	for _, f := range []struct {
		field string
		s     audiodirector.Sample
	}{{"video", req.Video}, {"audio", req.Audio}} {
		part, err := mw.CreateFormFile(f.field, f.s.Filename)
		if err != nil {
			return generation{}, err
		}
		if _, err := part.Write(f.s.Data); err != nil {
			return generation{}, err
		}
	}
	if err := mw.Close(); err != nil {
		return generation{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url("/v2/generate"), &buf)
	if err != nil {
		return generation{}, err
	}
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return s.send(r, key)
}

func (s *Client) status(ctx context.Context, key, id string) (generation, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url("/v2/generate/"+url.PathEscape(id))+"?wait=true&timeout=10", nil)
	if err != nil {
		return generation{}, err
	}
	return s.send(r, key)
}

func (s *Client) url(path string) string { return strings.TrimRight(s.cfg.BaseURL, "/") + path }

// send runs a request of the API and reads the generation it answers with.
func (s *Client) send(r *http.Request, key string) (generation, error) {
	r.Header.Set("x-api-key", key)
	r.Header.Set("Accept", "application/json")
	res, err := s.client.Do(r)
	if err != nil {
		return generation{}, err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return generation{}, err
	}
	if res.StatusCode/100 != 2 {
		return generation{}, retryAfter(res, &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Status: res.StatusCode, Message: httpclient.Snippet(data)})
	}
	var g generation
	if err := json.Unmarshal(data, &g); err != nil || g.ID == "" {
		return generation{}, &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Status: res.StatusCode, Message: "the answer is not a generation"}
	}
	return g, nil
}

// download fetches the output video. The API key is not sent: the URL is a signed link of the vendor's storage.
func (s *Client) download(ctx context.Context, link string) ([]byte, error) {
	if err := s.checkOutputURL(link); err != nil {
		return nil, &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Message: err.Error()}
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.fetcher.Do(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return nil, retryAfter(res, &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Status: res.StatusCode, Message: "download of the output failed"})
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxLipSyncBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxLipSyncBytes {
		return nil, &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Status: res.StatusCode, Message: "the output is empty or too large"}
	}
	return data, nil
}

// checkOutputURL refuses an output URL that is not an https URL of a public host.
func (s *Client) checkOutputURL(link string) error {
	if s.cfg.LocalDownloads {
		if u, err := url.Parse(link); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return errors.New("the output URL is not an http(s) URL")
		}
		return nil
	}
	if err := safefetch.CheckURL(link); err != nil {
		return errors.New(strings.Replace(err.Error(), "result URL", "output URL", 1))
	}
	return nil
}
