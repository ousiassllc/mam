// Package topaz is the Topaz Video API client (https://developer.topazlabs.com) that makes a video larger (Proteus, Iris, Starlight) or smoother
// (Apollo, Chronos) in the paid cloud (Issue #1031). It implements cloudenhance.Cloud.
//
// The flow of one job: POST /video/ quotes it (free); once the user confirmed the credits, PATCH /video/{id}/accept reserves them and returns the
// upload URL; the file is PUT to that URL; PATCH /video/{id}/complete-upload/ starts the processing; GET /video/{id}/status is polled until the
// job is complete; the result is downloaded. The API key (header X-API-Key) goes to api.topazlabs.com only: the upload and the download URLs are
// presigned, and nothing but the file is sent to them.
//
// A request that reserves or charges is never repeated (accept, complete-upload): an answer that was lost may have been processed. Only the GETs
// of a job that was accepted are repeated, until the time limit of the job.
package topaz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// CredentialProvider is the credential.Store key of the API key.
const CredentialProvider = "topaz"

// vendor is the name in the errors (modeladapter.ProviderError.Vendor).
const vendor = CredentialProvider

// maxAnswerBytes bounds a JSON answer of the API.
const maxAnswerBytes = 1 << 20

// Config is the Topaz setting; a zero field takes the DefaultConfig value.
type Config struct {
	BaseURL string
	// PollInterval is the wait between two status requests, and the first wait of a repeated one.
	PollInterval time.Duration
	// Timeout bounds one Run from the accept to the end of the download.
	Timeout time.Duration
	// MaxDownloadBytes bounds the result file.
	MaxDownloadBytes int64
	// LocalTransfers lets the upload and the download URLs be plain http and point to a local address. It exists for tests against httptest servers.
	LocalTransfers bool
}

// DefaultConfig is the setting used unless the caller changes it.
func DefaultConfig() Config {
	return Config{BaseURL: "https://api.topazlabs.com", PollInterval: 5 * time.Second, Timeout: 3 * time.Hour, MaxDownloadBytes: 2 << 30}
}

// Client is the Topaz Video API client.
type Client struct {
	cfg  Config
	keys credential.Store
	// api talks to the API, with the key. It refuses a redirect to another host (httpclient.New), so the key stays at Topaz.
	api httpclient.Doer
	// files moves the files to and from the URLs the API returned, without the key.
	files httpclient.Doer
}

var _ cloudenhance.Cloud = (*Client)(nil)

// New returns a Client that reads the key from keys on every call. A nil client selects httpclient.New for the API and a client that connects
// to public addresses only (safefetch.NewClient) for the files; a given client serves both. The api URL is not checked: it is the setting.
func New(cfg Config, keys credential.Store, client httpclient.Doer) *Client {
	def := DefaultConfig()
	if cfg.BaseURL == "" {
		cfg.BaseURL = def.BaseURL
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = def.PollInterval
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.MaxDownloadBytes <= 0 {
		cfg.MaxDownloadBytes = def.MaxDownloadBytes
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	files := client
	if client == nil {
		client = httpclient.New()
		files = client
		if !cfg.LocalTransfers {
			files = safefetch.NewClient()
		}
	}
	return &Client{cfg: cfg, keys: keys, api: client, files: files}
}

// Configured reports whether an API key is stored. A store that cannot answer counts as configured: the call itself reports the real error.
func (c *Client) Configured() bool {
	has, err := c.keys.Has(CredentialProvider)
	return err != nil || has
}

// Models are the models of the Video API that mam offers.
func (c *Client) Models() []cloudenhance.CloudModel {
	return append([]cloudenhance.CloudModel(nil), models...)
}

// key reads the API key. It is never put in an error or a log: what comes from outside is redacted with it (logging.Redact).
func (c *Client) key() (string, error) {
	key, err := c.keys.Get(CredentialProvider)
	if errors.Is(err, credential.ErrNotFound) {
		return "", ErrNoAPIKey
	}
	if err != nil {
		return "", fmt.Errorf("read Topaz API key: %w", err)
	}
	return key, nil
}

// call sends one request to the API with the key and decodes a 2xx answer into out (nil: the body is dropped). in, when set, is the JSON body.
// The error is a *modeladapter.ProviderError, or an *Error when Topaz named an errorCode.
func (c *Client) call(ctx context.Context, key, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode the Topaz request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("build the Topaz request: %w", err)
	}
	req.Header.Set("X-API-Key", key)
	req.Header.Set("Accept", "application/json")
	// Cloudflare in front of the API blocks the default agent of a library (errors.md, 403).
	req.Header.Set("User-Agent", "mam (https://github.com/ousiassllc/mam)")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.api.Do(req)
	if err != nil {
		return &modeladapter.ProviderError{Vendor: vendor, Err: redacted(httpclient.RedactURL(err), key)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxAnswerBytes+1))
	if err != nil {
		return &modeladapter.ProviderError{Vendor: vendor, Err: redacted(err, key)}
	}
	if len(data) > maxAnswerBytes {
		return &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusOK, Message: "the answer is too large"}
	}
	if res.StatusCode/100 != 2 {
		return c.apiError(res, data, key)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusOK, Message: "the answer is not JSON"}
	}
	return nil
}

// redacted is err with the key (and what looks like a secret) hidden in its text.
func redacted(err error, key string) error {
	if err == nil {
		return nil
	}
	if msg := logging.Redact(err.Error(), key); msg != err.Error() {
		return errors.New(msg)
	}
	return err
}

// repeat runs fn again while it fails with an unavailable answer (a network failure, 429, 5xx), until ctx ends: the job is accepted (and billed)
// already, so the time limit of the job, not a number of tries, bounds it. fn must be a GET.
func (c *Client) repeat(ctx context.Context, fn func() error) error {
	return resilience.Repeat(ctx, resilience.Backoff{Base: c.cfg.PollInterval, Max: resilience.JobRetryMaxDelay},
		resilience.Hooks{Unavailable: modeladapter.IsUnavailable, RetryAfter: retryAfter}, fn)
}

func retryAfter(err error) time.Duration {
	var pe *modeladapter.ProviderError
	if errors.As(err, &pe) {
		return pe.RetryAfter
	}
	return 0
}
