package topaz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// uploadContentType is what the documentation sends with the source whatever its container (video-quickstart: a presigned URL may be signed for it).
const uploadContentType = "video/mp4"

// checkURL refuses a URL of the answers of Topaz (the upload URL, the download URL) that is not an https URL of a public host: it comes from
// a remote answer, so it must not lead mam to plain http or to the local network.
func (c *Client) checkURL(raw string) error {
	if c.cfg.LocalTransfers {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return errors.New("the URL is not an http(s) URL")
		}
		return nil
	}
	return safefetch.CheckURL(raw)
}

// maxTransferTries bounds the tries of one upload or download. Unlike a poll, a transfer can fail for good in a way that looks like a network
// failure (a redirect that the client refuses, an address that the guard refuses), so it is not tried until the time limit of the job.
const maxTransferTries = 5

// repeatTransfer is repeat that gives up after maxTransferTries tries.
func (c *Client) repeatTransfer(ctx context.Context, fn func() error) error {
	tries := 0
	return resilience.Repeat(ctx, resilience.Backoff{Base: c.cfg.PollInterval, Max: resilience.JobRetryMaxDelay},
		resilience.Hooks{Unavailable: modeladapter.IsUnavailable, RetryAfter: retryAfter, Retryable: func(error) bool { tries++; return tries < maxTransferTries }}, fn)
}

// upload PUTs the source file to the presigned URL, streamed from the file with its Content-Length, and returns the ETag of the answer. No API
// key goes with it: the URL is the credential. The PUT of the same bytes to the same URL can be repeated, so a network failure or a 5xx is
// tried again a few times (maxTransferTries).
func (c *Client) upload(ctx context.Context, requestID, target, path string) (string, error) {
	if err := c.checkURL(target); err != nil {
		return "", transferFailed(requestID, "the upload URL was refused: "+err.Error(), err)
	}
	var etag string
	err := c.repeatTransfer(ctx, func() (err error) { etag, err = c.putFile(ctx, target, path); return err })
	var local *fsutil.LocalError
	switch {
	case errors.As(err, &local), errors.Is(err, os.ErrNotExist), errors.Is(err, os.ErrPermission):
		return "", err
	case err != nil:
		return "", transferFailed(requestID, "the upload of the source failed", err)
	}
	return etag, nil
}

// putFile is one try of the upload.
func (c *Client) putFile(ctx context.Context, target, path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // the source file of the result the service chose in the results directory, never a path the user typed
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", &fsutil.LocalError{Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, io.NopCloser(f))
	if err != nil {
		return "", fmt.Errorf("build the upload: %w", httpclient.RedactURL(err))
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", uploadContentType)
	res, err := c.files.Do(req)
	if err != nil {
		return "", &modeladapter.ProviderError{Vendor: vendor, Err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxAnswerBytes))
	if res.StatusCode/100 != 2 {
		return "", &modeladapter.ProviderError{
			Vendor: vendor, Status: res.StatusCode, Message: "the upload was refused",
			RetryAfter: httpclient.ParseRetryAfter(res.Header.Get("Retry-After")),
		}
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		return "", &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusOK, Message: "the answer of the upload has no ETag"}
	}
	return etag, nil
}

// download GETs the result from the URL of the status answer into dest and returns its size, at most MaxDownloadBytes. No API key goes with it.
// The GET can be repeated, so a network failure or a 5xx is tried again a few times (maxTransferTries). A partial file is removed.
func (c *Client) download(ctx context.Context, requestID, target, dest string) (int64, error) {
	if err := c.checkURL(target); err != nil {
		return 0, transferFailed(requestID, "the download URL was refused: "+err.Error(), err)
	}
	var n int64
	err := c.repeatTransfer(ctx, func() (err error) { n, err = c.getFile(ctx, target, dest); return err })
	if err != nil {
		_ = os.Remove(dest)
		var local *fsutil.LocalError
		if errors.As(err, &local) {
			return 0, err
		}
		return 0, transferFailed(requestID, "the download of the result failed", err)
	}
	if n == 0 { // a finished job always makes a file: an empty one is a broken transfer, not a result
		_ = os.Remove(dest)
		return 0, transferFailed(requestID, "the downloaded result file is empty", nil)
	}
	return n, nil
}

// getFile is one try of the download.
func (c *Client) getFile(ctx context.Context, target, dest string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, fmt.Errorf("build the download: %w", httpclient.RedactURL(err))
	}
	res, err := c.files.Do(req)
	if err != nil {
		return 0, &modeladapter.ProviderError{Vendor: vendor, Err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return 0, &modeladapter.ProviderError{
			Vendor: vendor, Status: res.StatusCode, Message: "the download of the result was refused",
			RetryAfter: httpclient.ParseRetryAfter(res.Header.Get("Retry-After")),
		}
	}
	tooLarge := &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusOK, Message: "the result file is too large"}
	if res.ContentLength > c.cfg.MaxDownloadBytes {
		return 0, tooLarge
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // the new result file the service chose in the results directory, never a path the user typed
	if err != nil {
		return 0, &fsutil.LocalError{Err: err}
	}
	n, err := io.Copy(localWriter{f}, io.LimitReader(res.Body, c.cfg.MaxDownloadBytes+1))
	if err == nil && n > c.cfg.MaxDownloadBytes {
		err = tooLarge
	}
	if err == nil {
		err = f.Sync()
		if err != nil {
			err = &fsutil.LocalError{Err: err}
		}
	}
	if cerr := f.Close(); err == nil && cerr != nil {
		err = &fsutil.LocalError{Err: cerr}
	}
	var local *fsutil.LocalError
	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, tooLarge), errors.As(err, &local):
		return 0, err
	}
	return 0, &modeladapter.ProviderError{Vendor: vendor, Err: httpclient.RedactURL(err)}
}

// localWriter marks the failures of w as the disk's (fsutil.LocalError), to tell them from a failing read of the body: the first is not
// repeated, the second is.
type localWriter struct{ w io.Writer }

func (l localWriter) Write(p []byte) (int, error) {
	n, err := l.w.Write(p)
	if err != nil {
		err = &fsutil.LocalError{Err: err}
	}
	return n, err
}
