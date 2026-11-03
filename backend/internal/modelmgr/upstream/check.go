package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// Level is how far a pin was verified against the upstream.
type Level string

// The levels, from the strongest. A pin that has a problem has no level.
const (
	// LevelSHA256 says the upstream's SHA-256 (the Hugging Face LFS etag, or the hash of a download) equals the pinned one, and so does the size.
	LevelSHA256 Level = "sha256"
	// LevelSize says the file is there and has the pinned size; its SHA-256 can be told only by downloading it.
	LevelSize Level = "size"
	// LevelAlive says the file is there; the upstream does not say how big it is.
	LevelAlive Level = "alive"
	// LevelSkipped says the file is behind a Hugging Face login and no token was given: it was not asked.
	LevelSkipped Level = "skipped"
)

// Result is the outcome of one pin: Problems is empty when the pin still holds, and Level then says how far it was verified.
type Result struct {
	Pin      Pin
	Level    Level
	Problems []string
}

// Checker checks pins against their upstream. The zero value needs only a Client.
type Checker struct {
	// Client is the base HTTP client (the command passes safefetch.NewClient(): public addresses only). It never follows a redirect by itself: the
	// check follows them one at a time, so that it can read the first response's headers and test every hop against the host list.
	Client *http.Client
	// Allow tests the URL of a request and of each of its redirects; nil selects the host lists of the real downloads (fetch.AllowedURL, fetch.PackageURL).
	Allow func(Hosts, *url.URL) error
	// Concurrency is the number of requests in flight (at least 1).
	Concurrency int
	// Timeout bounds one probe request; DownloadTimeout one download for HashBelow. 0 for none.
	Timeout         time.Duration
	DownloadTimeout time.Duration
	// Retries is how many times a request that failed on the network or with 429 / 5xx is repeated, RetryDelay apart.
	Retries    int
	RetryDelay time.Duration
	// HFToken is the Hugging Face token for the gated files (Pin.Gated); it is sent to huggingface.co only, never to a CDN a redirect leads to.
	HFToken string
	// HashBelow makes the check download a file that has no SHA-256 in its upstream's headers and is at most this many bytes, and hash it (0: never).
	HashBelow int64
}

const maxRedirects = 8

// Run checks every pin and returns the results in the order of pins.
func (c Checker) Run(ctx context.Context, pins []Pin) []Result {
	results := make([]Result, len(pins))
	slots := make(chan struct{}, max(c.Concurrency, 1))
	var wg sync.WaitGroup
	for i, p := range pins {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			results[i] = c.Check(ctx, p)
		}()
	}
	wg.Wait()
	return results
}

// Check checks one pin.
func (c Checker) Check(ctx context.Context, p Pin) Result {
	r := Result{Pin: p}
	if p.Gated && c.HFToken == "" {
		r.Level = LevelSkipped
		return r
	}
	fail := func(format string, args ...any) Result {
		r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
		return r
	}
	o, err := c.observe(ctx, p)
	switch {
	case err != nil:
		return fail("unreachable: %v", err)
	case o.status == http.StatusNotFound || o.status == http.StatusGone:
		return fail("gone: HTTP %d", o.status)
	case o.status < 200 || o.status >= 400:
		return fail("not downloadable: HTTP %d", o.status)
	}
	if o.size >= 0 && o.size != p.Size {
		r.Problems = append(r.Problems, fmt.Sprintf("size changed: pinned %d bytes, upstream %d bytes", p.Size, o.size))
	}
	sum := o.sha256
	if sum == "" && c.HashBelow > 0 && p.Size <= c.HashBelow && len(r.Problems) == 0 {
		if sum, err = c.download(ctx, p); err != nil {
			return fail("download for the SHA-256 failed: %v", err)
		}
	}
	if sum != "" && sum != p.SHA256 {
		r.Problems = append(r.Problems, fmt.Sprintf("sha256 changed: pinned %s, upstream %s", p.SHA256, sum))
	}
	switch {
	case len(r.Problems) > 0:
	case sum != "":
		r.Level = LevelSHA256
	case o.size >= 0:
		r.Level = LevelSize
	default:
		r.Level = LevelAlive
	}
	return r
}

// observation is what the upstream says about a file: the status of the end of the redirect chain, the size (-1 when unknown) and the
// SHA-256 ("" when unknown).
type observation struct {
	status int
	size   int64
	sha256 string
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// observe asks the upstream with HEAD and, when that does not tell the size (or the server refuses HEAD), with a one-byte range request.
func (c Checker) observe(ctx context.Context, p Pin) (observation, error) {
	o, err := c.retry(ctx, func() (observation, error) { return c.chase(ctx, p, http.MethodHead) })
	if err != nil {
		return o, err
	}
	if o.status == http.StatusNotFound || o.status == http.StatusGone {
		return o, nil
	}
	if o.size >= 0 && o.status >= 200 && o.status < 400 {
		return o, nil
	}
	g, err := c.retry(ctx, func() (observation, error) { return c.chase(ctx, p, http.MethodGet) })
	if err != nil {
		return g, err
	}
	if g.sha256 == "" {
		g.sha256 = o.sha256
	}
	return g, nil
}

// retry repeats do while it fails on the network or the server answers 429 or 5xx.
func (c Checker) retry(ctx context.Context, do func() (observation, error)) (observation, error) {
	for attempt := 0; ; attempt++ {
		o, err := do()
		transient := err != nil && !errors.Is(err, errRefused) || o.status == http.StatusTooManyRequests || o.status >= 500
		if !transient || attempt >= c.Retries || ctx.Err() != nil {
			return o, err
		}
		select {
		case <-ctx.Done():
			return o, ctx.Err()
		case <-time.After(c.RetryDelay):
		}
	}
}

// errRefused marks a URL that the host list refuses: the same on every attempt.
var errRefused = errors.New("refused")

// follow sends method and follows the redirects one at a time, testing every hop against the host list. It returns the first response
// that is not a redirect, or the first response itself when stopAtFirst accepts it (the caller closes the body). probe asks for one byte
// of a GET only.
func (c Checker) follow(ctx context.Context, p Pin, method string, probe bool, stopAtFirst func(*http.Response) bool) (*http.Response, error) {
	next := p.URL
	for hop := 0; ; hop++ {
		resp, err := c.send(ctx, p, method, next, probe)
		if err != nil {
			return nil, err
		}
		loc := resp.Header.Get("Location")
		isRedirect := loc != "" && resp.StatusCode >= 300 && resp.StatusCode < 400
		stop := hop == 0 && stopAtFirst != nil && stopAtFirst(resp)
		if !isRedirect || stop {
			return resp, nil
		}
		_ = resp.Body.Close()
		if hop >= maxRedirects {
			return nil, errors.New("too many redirects")
		}
		base, _ := url.Parse(next)
		target, err := base.Parse(loc)
		if err != nil {
			return nil, fmt.Errorf("bad redirect %q: %w", loc, err)
		}
		next = target.String()
	}
}

// chase asks with method (HEAD, or a GET of the first byte) what the upstream says about the file. The first response of a Hugging Face
// file carries its size and SHA-256 (X-Linked-Size, X-Linked-Etag), which the CDN behind the redirect does not repeat: a file that has them
// is not followed to the CDN (its signed address is made for a GET).
func (c Checker) chase(ctx context.Context, p Pin, method string) (observation, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	var linked observation
	var hasLinked bool
	resp, err := c.follow(ctx, p, method, method == http.MethodGet, func(first *http.Response) bool {
		linked, hasLinked = linkedFacts(first)
		return hasLinked
	})
	if err != nil {
		return observation{size: -1}, err
	}
	defer resp.Body.Close()
	if hasLinked && resp.Header.Get("Location") != "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return observation{status: resp.StatusCode, size: linked.size, sha256: linked.sha256}, nil
	}
	return observation{status: resp.StatusCode, size: bodySize(resp), sha256: linked.sha256}, nil
}

func (c Checker) send(ctx context.Context, p Pin, method, target string, probe bool) (*http.Response, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errRefused, err)
	}
	allow := c.Allow
	if allow == nil {
		allow = defaultAllow
	}
	if err := allow(p.Hosts, u); err != nil {
		return nil, fmt.Errorf("%w: %w", errRefused, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity") // the size of the file, not of a compressed body
	if probe {
		req.Header.Set("Range", "bytes=0-0")
	}
	if host := strings.ToLower(u.Hostname()); p.Gated && c.HFToken != "" && (host == "huggingface.co" || host == "hf.co") {
		req.Header.Set("Authorization", "Bearer "+c.HFToken)
	}
	cl := *c.Client
	cl.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return cl.Do(req)
}

func defaultAllow(h Hosts, u *url.URL) error {
	if h == PackageHosts {
		return fetch.PackageURL(u)
	}
	return fetch.AllowedURL(u)
}

// linkedFacts reads what Hugging Face says on the first response about the file behind it: X-Linked-Size and X-Linked-Etag, which is the
// SHA-256 of an LFS file (a file stored in git has neither: its etag is a git object id, which says nothing about the SHA-256).
func linkedFacts(resp *http.Response) (observation, bool) {
	size := int64(-1)
	if v := resp.Header.Get("X-Linked-Size"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			size = n
		}
	}
	sum := strings.ToLower(strings.Trim(strings.TrimPrefix(resp.Header.Get("X-Linked-Etag"), "W/"), `"`))
	if !sha256Hex.MatchString(sum) {
		sum = ""
	}
	return observation{size: size, sha256: sum}, size >= 0 || sum != ""
}

// bodySize is the size of the file the response is about: the whole size of a 206 (Content-Range: bytes 0-0/N), else Content-Length.
func bodySize(resp *http.Response) int64 {
	if resp.StatusCode == http.StatusPartialContent {
		if _, total, ok := strings.Cut(resp.Header.Get("Content-Range"), "/"); ok {
			if n, err := strconv.ParseInt(total, 10, 64); err == nil && n >= 0 {
				return n
			}
		}
		return -1
	}
	return resp.ContentLength
}

// download fetches the file and returns its SHA-256, reading at most one byte more than the pinned size.
func (c Checker) download(ctx context.Context, p Pin) (string, error) {
	if c.DownloadTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.DownloadTimeout)
		defer cancel()
	}
	resp, err := c.follow(ctx, p, http.MethodGet, false, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(resp.Body, p.Size+1))
	if err != nil {
		return "", err
	}
	if n != p.Size {
		return "", fmt.Errorf("size changed: pinned %d bytes, upstream sent more or less (%d read)", p.Size, n)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
