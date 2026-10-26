// Package fetch downloads one pinned file of the model manager (a model file from Hugging Face, the ComfyUI
// archive from GitHub): a size and SHA-256 fixed in the catalog, resumed from "<dest>.part" with a Range
// request, checked before the file appears at its destination, and refused up front when the disk lacks the
// room. The download rules are those of a vendor result URL (https, public addresses only; safefetch) with a
// fixed list of hosts and the redirects such CDNs need; a Hugging Face token is sent to huggingface.co only.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// Source is a pinned file: where it is, the SHA-256 it must have and its size.
type Source struct {
	URL    string
	SHA256 string
	Size   int64
}

// Progress reports a download: the bytes of the file in place (a resumed file counts from its start) and its size.
type Progress func(done, total int64)

// Options of Download. The zero value is the production setting.
type Options struct {
	// Client sends the requests. Nil selects the client of NewClient.
	Client httpclient.Doer
	// CheckURL accepts the URL of the request and of every redirect. Nil selects AllowedURL.
	CheckURL func(*url.URL) error
	// Token returns the Hugging Face token ("" for none). It is sent to AuthHost only. Nil: never.
	Token func() string
	// AuthHost is the one host that gets the token (default "huggingface.co").
	AuthHost string
	// FreeBytes reports the free space at a path. Nil selects fsutil.FreeBytes.
	FreeBytes func(path string) (uint64, error)
	// Progress receives the progress; it must not block.
	Progress Progress
	// IdleTimeout aborts a transfer that delivers no byte for this long (default 60 s).
	IdleTimeout time.Duration
}

// Failures of a download. A wrapped *HTTPError, ErrChecksum, ErrSize and *NoSpaceError are definitive (the part
// file is gone, running it again will not help without a change); the others (network, cancel) keep the part
// file so the next run resumes.
var (
	// ErrChecksum: the content has another SHA-256 than the catalog pinned. The part file was removed.
	ErrChecksum = errors.New("fetch: sha256 mismatch")
	// ErrSize: the server delivered more or fewer bytes than the catalog pinned. The part file was removed.
	ErrSize = errors.New("fetch: size mismatch")
)

// HTTPError is an answer other than a success.
type HTTPError struct {
	Status int
	URL    string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("fetch: %s answered %d", e.URL, e.Status) }

// Definitive reports whether the same request will fail again: any 4xx but request timeout and rate limit.
func (e *HTTPError) Definitive() bool {
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusRequestTimeout && e.Status != http.StatusTooManyRequests
}

// NoSpaceError reports that the disk has less free space than the download needs.
type NoSpaceError struct {
	Need, Free uint64
	Path       string
}

func (e *NoSpaceError) Error() string {
	return fmt.Sprintf("fetch: not enough free disk space at %s: need %d bytes, %d free", e.Path, e.Need, e.Free)
}

// PartPath is the file a download is written to until it is complete.
func PartPath(dest string) string { return dest + ".part" }

// Verified reports whether dest is the pinned file: its size and SHA-256 match. A missing file is false, nil.
func Verified(dest string, src Source) (bool, error) {
	st, err := os.Stat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if st.Size() != src.Size {
		return false, nil
	}
	sum, err := hashFile(dest)
	return err == nil && strings.EqualFold(sum, src.SHA256), err
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- a path the model manager chose
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Download makes dest the pinned file. A dest that is the pinned file already is left alone (the call is
// idempotent); a dest that is not is replaced. The content is written to dest+".part", continued with a Range
// request when a part file is there, and renamed to dest after its size and SHA-256 were checked. ctx ends the
// transfer and keeps the part file.
func Download(ctx context.Context, dest string, src Source, o Options) error {
	if ok, err := Verified(dest, src); err != nil {
		return err
	} else if ok {
		report(o, src.Size, src.Size)
		return nil
	}
	part := PartPath(dest)
	have, err := partSize(part, src.Size)
	if err != nil {
		return err
	}
	if err := checkSpace(dest, src.Size-have, o); err != nil {
		return err
	}
	if err := os.MkdirAll(dirOf(dest), 0o750); err != nil {
		return fmt.Errorf("fetch: create %s: %w", dirOf(dest), err)
	}
	err = transfer(ctx, part, have, src, o)
	var he *HTTPError
	switch {
	case errors.Is(err, ErrChecksum), errors.Is(err, ErrSize), errors.As(err, &he) && he.Definitive():
		_ = os.Remove(part)
	}
	if err != nil {
		return err
	}
	_ = os.Remove(dest) // a stale or corrupt file; os.Rename does not replace on every platform
	if err := os.Rename(part, dest); err != nil {
		return fmt.Errorf("fetch: finish %s: %w", dest, err)
	}
	return nil
}

func report(o Options, done, total int64) {
	if o.Progress != nil {
		o.Progress(done, total)
	}
}

// partSize is the size of a usable part file; one that is larger than the file is dropped.
func partSize(part string, total int64) (int64, error) {
	st, err := os.Stat(part)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return 0, nil
	case err != nil:
		return 0, err
	case st.Size() > total:
		return 0, os.Remove(part)
	}
	return st.Size(), nil
}

func checkSpace(dest string, need int64, o Options) error {
	free := o.FreeBytes
	if free == nil {
		free = fsutil.FreeBytes
	}
	have, err := free(dirOf(dest))
	if err != nil {
		return fmt.Errorf("fetch: free space: %w", err)
	}
	if need > 0 && have < uint64(need) {
		return &NoSpaceError{Need: uint64(need), Free: have, Path: dirOf(dest)}
	}
	return nil
}

func (o Options) client() httpclient.Doer {
	if o.Client != nil {
		return o.Client
	}
	return NewClient(o.check(), o.authHost())
}

func (o Options) check() func(*url.URL) error {
	if o.CheckURL != nil {
		return o.CheckURL
	}
	return AllowedURL
}

func (o Options) authHost() string {
	if o.AuthHost != "" {
		return o.AuthHost
	}
	return "huggingface.co"
}

// transfer writes src to part, which holds its first have bytes, and checks the result.
func transfer(ctx context.Context, part string, have int64, src Source, o Options) error {
	u, err := url.Parse(src.URL)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := o.check()(u); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	if o.Token != nil && strings.EqualFold(u.Hostname(), o.authHost()) {
		if tok := o.Token(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	res, err := o.client().Do(req)
	if err != nil {
		return httpclient.RedactURL(err)
	}
	defer func() { _ = res.Body.Close() }()
	resumed, err := accept(res, have, src)
	if errors.Is(err, errRestart) { // a part file the server cannot continue: start again
		_ = os.Remove(part)
		return transfer(ctx, part, 0, src, o)
	}
	if err != nil {
		return err
	}
	if !resumed {
		have = 0
	}
	return write(ctx, cancel, part, have, res.Body, src, o)
}

var errRestart = errors.New("restart")

// accept decides what the answer is: a 206 that continues at have is appended; a 200 is the whole file again;
// a 416 means the part file is unusable.
func accept(res *http.Response, have int64, src Source) (resumed bool, err error) {
	switch res.StatusCode {
	case http.StatusPartialContent:
		start, ok := rangeStart(res.Header.Get("Content-Range"))
		if !ok || start != have {
			return false, fmt.Errorf("fetch: the server continued at the wrong offset (%q)", res.Header.Get("Content-Range"))
		}
		return true, nil
	case http.StatusOK:
		return false, nil
	case http.StatusRequestedRangeNotSatisfiable:
		if have > 0 {
			return false, errRestart
		}
	}
	return false, &HTTPError{Status: res.StatusCode, URL: redact(res.Request, src.URL)}
}

func redact(req *http.Request, fallback string) string {
	if req != nil && req.URL != nil {
		return req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
	}
	return fallback
}

// rangeStart is the first byte of a Content-Range value "bytes 100-199/200".
func rangeStart(v string) (int64, bool) {
	rest, ok := strings.CutPrefix(v, "bytes ")
	if !ok {
		return 0, false
	}
	first, _, _ := strings.Cut(rest, "-")
	n, err := strconv.ParseInt(first, 10, 64)
	return n, err == nil
}
