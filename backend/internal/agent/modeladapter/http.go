package modeladapter

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

const (
	// maxJSONBytes bounds a JSON answer (images come back base64-encoded).
	maxJSONBytes = 64 << 20
	// maxDownloadBytes bounds a downloaded image or video (the upload limit for videos, non-functional.md §5).
	maxDownloadBytes = 300 << 20
)

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
	client  httpclient.Doer
	// fetcher downloads the result files (their URLs come from the vendor's answers).
	fetcher httpclient.Doer
	// localDownloads lets a result URL be plain http and point to a local address (tests).
	localDownloads bool
	// pollEvery is the wait between two status requests of an asynchronous job.
	pollEvery time.Duration
	// stagingDir is where downloaded videos are staged until the caller stores them ("" selects the
	// system temporary directory).
	stagingDir string
	// maxDownload bounds one downloaded file (zero selects maxDownloadBytes).
	maxDownload int64
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

// Configured reports whether an API key is stored for the vendor. A store that cannot answer counts
// as configured: the call itself reports the real error.
func (a api) Configured() bool {
	has, err := a.keys.Has(string(a.vendor))
	return err != nil || has
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
		return nil, &ProviderError{Vendor: string(a.vendor), Err: httpclient.RedactURL(err)}
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
			Vendor: string(a.vendor), Status: res.StatusCode, Message: httpclient.Snippet(data),
			RetryAfter: httpclient.ParseRetryAfter(res.Header.Get("Retry-After")),
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

// download fetches a generated video from the URL the vendor returned and stages it in a file:
// the body is streamed to disk, never held in memory. It only runs after the job was submitted,
// so a transient failure is retried here (the GET is repeatable) and any error it finally returns
// is marked errAfterSubmit: the job must not be submitted again.
func (a api) download(ctx context.Context, fileURL string) (Blob, string, error) {
	return a.fetch(ctx, fileURL, a.stage)
}

// downloadBytes is download for an image, which is small enough to keep in memory.
func (a api) downloadBytes(ctx context.Context, fileURL string) ([]byte, string, error) {
	blob, mime, err := a.fetch(ctx, fileURL, a.collect)
	return blob.data, mime, err
}

// sink turns the body of a download, at most limit bytes of it, into a Blob.
type sink func(body io.Reader, limit int64) (Blob, error)

func (a api) fetch(ctx context.Context, fileURL string, into sink) (Blob, string, error) {
	var (
		blob Blob
		mime string
	)
	err := a.repeat(ctx, func() (err error) {
		blob, mime, err = a.fetchOnce(ctx, fileURL, into)
		return err
	})
	if err != nil {
		return Blob{}, "", afterSubmit(err)
	}
	return blob, mime, nil
}

func (a api) fetchOnce(ctx context.Context, fileURL string, into sink) (Blob, string, error) {
	if err := a.checkResultURL(fileURL); err != nil {
		return Blob{}, "", &ProviderError{Vendor: string(a.vendor), Status: http.StatusOK, Message: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return Blob{}, "", fmt.Errorf("build %s download: %w", a.vendor, err)
	}
	res, err := a.fetcher.Do(req)
	if err != nil {
		return Blob{}, "", &ProviderError{Vendor: string(a.vendor), Err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return Blob{}, "", &ProviderError{Vendor: string(a.vendor), Status: res.StatusCode, Message: "download of the result failed", RetryAfter: httpclient.ParseRetryAfter(res.Header.Get("Retry-After"))}
	}
	limit := cmp.Or(a.maxDownload, maxDownloadBytes)
	tooLarge := &ProviderError{Vendor: string(a.vendor), Status: http.StatusOK, Message: "the result file is too large"}
	if res.ContentLength > limit {
		return Blob{}, "", tooLarge
	}
	blob, err := into(res.Body, limit)
	switch {
	case errors.Is(err, errTooLarge):
		return Blob{}, "", tooLarge
	case err != nil:
		return Blob{}, "", &ProviderError{Vendor: string(a.vendor), Err: err}
	}
	return blob, res.Header.Get("Content-Type"), nil
}

var errTooLarge = errors.New("the body is larger than the limit")

// collect is the sink that keeps the body in memory.
func (a api) collect(body io.Reader, limit int64) (Blob, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return Blob{}, err
	}
	if int64(len(data)) > limit {
		return Blob{}, errTooLarge
	}
	return BlobOf(data), nil
}

// stage is the sink that streams the body into a new file of the staging directory. The file is
// synced, and removed again when the body cannot be read completely.
func (a api) stage(body io.Reader, limit int64) (blob Blob, err error) {
	dir := a.stagingDir
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Blob{}, fmt.Errorf("create staging directory: %w", err)
		}
	}
	f, err := fsutil.CreateStagingTemp(dir) // RemoveStaleTemp sweeps it after a crash
	if err != nil {
		return Blob{}, fmt.Errorf("create staging file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	n, err := io.Copy(f, io.LimitReader(body, limit+1))
	if err != nil {
		return Blob{}, err
	}
	if n > limit {
		return Blob{}, errTooLarge
	}
	if err = f.Sync(); err != nil {
		return Blob{}, fmt.Errorf("sync staging file: %w", err)
	}
	if err = f.Close(); err != nil {
		return Blob{}, fmt.Errorf("close staging file: %w", err)
	}
	return Blob{path: f.Name(), size: n}, nil
}

// ownsURL reports whether target is a URL of the vendor, the only kind a request carrying the API
// key may go to: it has the scheme and port of the vendor's base URL and its host is the base host
// or a sub-domain of the base host's parent domain (api.bfl.ai also answers from api.eu.bfl.ai).
func (a api) ownsURL(target string) bool {
	base, err := url.Parse(a.baseURL)
	if err != nil || base.Hostname() == "" {
		return false
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme != base.Scheme || u.Port() != base.Port() || u.User != nil {
		return false
	}
	host, baseHost := strings.ToLower(u.Hostname()), strings.ToLower(base.Hostname())
	if host == baseHost {
		return true
	}
	if _, err := netip.ParseAddr(baseHost); err == nil {
		return false
	}
	parent := baseHost
	if labels := strings.Split(baseHost, "."); len(labels) >= 3 {
		parent = strings.Join(labels[1:], ".")
	}
	return strings.HasSuffix(host, "."+parent)
}

// checkResultURL refuses a result URL that is not an https URL of a public host: it comes from the
// vendor's answer, so it must not lead the download to plain http or to the local network.
func (a api) checkResultURL(raw string) error {
	if a.localDownloads {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return errors.New("the result URL is not an http(s) URL")
		}
		return nil
	}
	return safefetch.CheckURL(raw)
}

// repeat runs fn again while it fails with an unavailable answer, until ctx ends (resilience.Repeat):
// the job is billed already, so the call's timeout, not a number of tries, bounds it. fn must be a GET.
func (a api) repeat(ctx context.Context, fn func() error) error {
	return resilience.Repeat(ctx, resilience.Backoff{Base: a.pollEvery, Max: resilience.JobRetryMaxDelay},
		resilience.Hooks{Unavailable: unavailable, RetryAfter: retryAfter}, fn)
}

// poll calls check until it reports done or fails, waiting pollEvery between calls. It only runs
// after the job was submitted: a transient failure of one status request is retried (it is a GET
// of the same job), and any error it finally returns is marked errAfterSubmit so the whole call
// is not repeated, which would submit, and be billed for, the job a second time.
func (a api) poll(ctx context.Context, check func(ctx context.Context) (done bool, err error)) error {
	for {
		var done bool
		err := a.repeat(ctx, func() (err error) {
			done, err = check(ctx)
			return err
		})
		if err != nil {
			return afterSubmit(err)
		}
		if done {
			return nil
		}
		select {
		case <-time.After(a.pollEvery):
		case <-ctx.Done():
			return afterSubmit(ctx.Err())
		}
	}
}

// failed reports a job that the vendor finished without a result (content policy, internal error).
func (a api) failed(reason string) error {
	return &ProviderError{Vendor: string(a.vendor), Status: http.StatusUnprocessableEntity, Message: reason}
}

func (a api) empty(what string) error {
	return &ProviderError{Vendor: string(a.vendor), Status: http.StatusOK, Message: "the answer has no " + what}
}
