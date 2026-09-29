package modeladapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

const (
	// maxJSONBytes bounds a JSON answer (images come back base64-encoded).
	maxJSONBytes = 64 << 20
	// maxDownloadBytes bounds a downloaded image or video (the upload limit for videos, non-functional.md §5).
	maxDownloadBytes = 300 << 20
)

// Doer is the part of *http.Client the backends use.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Backends are the HTTP backends of every model.
type Backends struct {
	Image map[domain.ModelName]ImageBackend
	Video map[domain.ModelName]VideoBackend
}

// api is what the vendor backends share.
type api struct {
	vendor  Vendor
	baseURL string
	keys    credential.Store
	client  Doer
	// pollEvery is the wait between two status requests of an asynchronous job.
	pollEvery time.Duration
}

func (a api) key() (string, error) {
	key, err := a.keys.Get(string(a.vendor))
	if errors.Is(err, credential.ErrNotFound) {
		return "", fmt.Errorf("%w: %s", ErrNoAPIKey, a.vendor)
	}
	if err != nil {
		return "", fmt.Errorf("read %s API key: %w", a.vendor, err)
	}
	return key, nil
}

// request sends a request and returns the body of a 2xx answer. url is absolute.
func (a api) request(ctx context.Context, method, target string, header map[string]string, body []byte, limit int64) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", a.vendor, err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := a.client.Do(req)
	if err != nil {
		return nil, &ProviderError{Vendor: string(a.vendor), Err: redactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, &ProviderError{Vendor: string(a.vendor), Err: err}
	}
	if int64(len(data)) > limit {
		return nil, &ProviderError{Vendor: string(a.vendor), Status: http.StatusOK, Message: "the answer is too large"}
	}
	if res.StatusCode/100 != 2 {
		return nil, &ProviderError{
			Vendor: string(a.vendor), Status: res.StatusCode, Message: snippet(data),
			RetryAfter: parseRetryAfter(res.Header.Get("Retry-After")),
		}
	}
	return data, nil
}

func jsonHeader(h map[string]string) map[string]string {
	out := map[string]string{"Content-Type": "application/json"}
	for k, v := range h {
		out[k] = v
	}
	return out
}

// download fetches a generated file from the URL the vendor returned.
func (a api) download(ctx context.Context, fileURL string) ([]byte, string, error) {
	u, err := url.Parse(fileURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, "", &ProviderError{Vendor: string(a.vendor), Status: http.StatusOK, Message: "the result URL is not an http(s) URL"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build %s download: %w", a.vendor, err)
	}
	res, err := a.client.Do(req)
	if err != nil {
		return nil, "", &ProviderError{Vendor: string(a.vendor), Err: redactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return nil, "", &ProviderError{Vendor: string(a.vendor), Status: res.StatusCode, Message: "download of the result failed"}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, "", &ProviderError{Vendor: string(a.vendor), Err: err}
	}
	if int64(len(data)) > maxDownloadBytes {
		return nil, "", &ProviderError{Vendor: string(a.vendor), Status: http.StatusOK, Message: "the result file is too large"}
	}
	return data, res.Header.Get("Content-Type"), nil
}

// poll calls check until it reports done or fails, waiting pollEvery between calls.
func (a api) poll(ctx context.Context, check func(ctx context.Context) (done bool, err error)) error {
	for {
		done, err := check(ctx)
		if err != nil || done {
			return err
		}
		select {
		case <-time.After(a.pollEvery):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// failed reports a job that the vendor finished without a result (content policy, internal error).
func (a api) failed(reason string) error {
	return &ProviderError{Vendor: string(a.vendor), Status: http.StatusUnprocessableEntity, Message: reason}
}

// redactURL drops the request URL from a transport error: it must not leak into logs or the chat.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if r := []rune(s); len(r) > 300 {
		return string(r[:300]) + "…"
	}
	return s
}

func parseRetryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func (a api) empty(what string) error {
	return &ProviderError{Vendor: string(a.vendor), Status: http.StatusOK, Message: "the answer has no " + what}
}
