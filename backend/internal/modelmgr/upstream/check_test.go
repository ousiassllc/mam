package upstream_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/upstream"
)

const pinnedSum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// allowAll lets the httptest server (plain http on 127.0.0.1) through: the host lists have their own tests below.
func allowAll(upstream.Hosts, *url.URL) error { return nil }

func checker() upstream.Checker {
	return upstream.Checker{Client: http.DefaultClient, Allow: allowAll, Concurrency: 4}
}

func pin(srv *httptest.Server, path string, size int64, sum string) upstream.Pin {
	return upstream.Pin{Label: "test " + path, URL: srv.URL + path, SHA256: sum, Size: size}
}

// hfLike answers like Hugging Face: a redirect whose headers carry the file's size and SHA-256 (the CDN behind it is never asked).
func hfLike(size int64, sum string, cdnHits *atomic.Int32) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/cdn", func(w http.ResponseWriter, _ *http.Request) {
		cdnHits.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/missing") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Linked-Size", strconv.FormatInt(size, 10))
		w.Header().Set("X-Linked-Etag", `"`+sum+`"`)
		http.Redirect(w, r, "/cdn", http.StatusFound)
	})
	return mux
}

func TestCheck_HuggingFaceHeadersProveSizeAndSHA256WithoutADownload(t *testing.T) {
	var cdn atomic.Int32
	srv := httptest.NewServer(hfLike(1234, pinnedSum, &cdn))
	defer srv.Close()

	r := checker().Check(context.Background(), pin(srv, "/model.safetensors", 1234, pinnedSum))

	assert.Empty(t, r.Problems)
	assert.Equal(t, upstream.LevelSHA256, r.Level)
	assert.Zero(t, cdn.Load(), "the signed CDN address is not followed when the first response says it all")
}

func TestCheck_ReportsAFileThatIsGone(t *testing.T) {
	var cdn atomic.Int32
	srv := httptest.NewServer(hfLike(1, pinnedSum, &cdn))
	defer srv.Close()

	r := checker().Check(context.Background(), pin(srv, "/missing", 1, pinnedSum))

	require.Len(t, r.Problems, 1)
	assert.Contains(t, r.Problems[0], "gone: HTTP 404")
}

func TestCheck_ReportsAChangedSize(t *testing.T) {
	var cdn atomic.Int32
	srv := httptest.NewServer(hfLike(2000, pinnedSum, &cdn))
	defer srv.Close()

	r := checker().Check(context.Background(), pin(srv, "/model.safetensors", 1234, pinnedSum))

	require.Len(t, r.Problems, 1)
	assert.Contains(t, r.Problems[0], "size changed: pinned 1234 bytes, upstream 2000 bytes")
}

func TestCheck_ReportsAChangedSHA256(t *testing.T) {
	var cdn atomic.Int32
	other := strings.Repeat("b", 64)
	srv := httptest.NewServer(hfLike(1234, other, &cdn))
	defer srv.Close()

	r := checker().Check(context.Background(), pin(srv, "/model.safetensors", 1234, pinnedSum))

	require.Len(t, r.Problems, 1)
	assert.Contains(t, r.Problems[0], "sha256 changed: pinned "+pinnedSum+", upstream "+other)
}

func TestCheck_AFileWithoutAShaIsVerifiedByItsContentLengthOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "77")
	}))
	defer srv.Close()

	r := checker().Check(context.Background(), pin(srv, "/x.whl", 77, pinnedSum))

	assert.Empty(t, r.Problems)
	assert.Equal(t, upstream.LevelSize, r.Level, "the SHA-256 of such a file can be told by a download only")
}

func TestCheck_AServerThatRefusesHeadIsAskedForOneByteInstead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		assert.Equal(t, "bytes=0-0", r.Header.Get("Range"))
		w.Header().Set("Content-Range", "bytes 0-0/5000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0})
	}))
	defer srv.Close()

	ok := checker().Check(context.Background(), pin(srv, "/x", 5000, pinnedSum))
	changed := checker().Check(context.Background(), pin(srv, "/x", 4000, pinnedSum))

	assert.Empty(t, ok.Problems)
	assert.Equal(t, upstream.LevelSize, ok.Level)
	require.Len(t, changed.Problems, 1)
	assert.Contains(t, changed.Problems[0], "upstream 5000 bytes")
}

func contentServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(body))
		}
	}))
}

func TestCheck_SmallFilesWithoutAShaAreDownloadedAndHashed(t *testing.T) {
	const body = "hello weights"
	sum := sha256.Sum256([]byte(body))
	srv := contentServer(body)
	defer srv.Close()
	c := checker()
	c.HashBelow = 1024

	same := c.Check(context.Background(), pin(srv, "/f", int64(len(body)), hex.EncodeToString(sum[:])))
	changed := c.Check(context.Background(), pin(srv, "/f", int64(len(body)), pinnedSum))
	tooBig := c.Check(context.Background(), pin(srv, "/f", 1<<20, pinnedSum)) // above HashBelow, and not the size either

	assert.Empty(t, same.Problems)
	assert.Equal(t, upstream.LevelSHA256, same.Level)
	require.Len(t, changed.Problems, 1)
	assert.Contains(t, changed.Problems[0], "sha256 changed")
	require.Len(t, tooBig.Problems, 1)
	assert.Contains(t, tooBig.Problems[0], "size changed")
}

func TestCheck_ARedirectToAHostThatTheCatalogDoesNotAllowIsRefused(t *testing.T) {
	var cdn atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdn.Add(1)
		http.Redirect(w, r, "https://evil.example/file", http.StatusFound)
	}))
	defer srv.Close()
	c := checker()
	c.Allow = func(_ upstream.Hosts, u *url.URL) error {
		if u.Hostname() == "evil.example" {
			return assert.AnError
		}
		return nil
	}

	r := c.Check(context.Background(), pin(srv, "/x", 1, pinnedSum))

	require.Len(t, r.Problems, 1)
	assert.Contains(t, r.Problems[0], "unreachable: refused")
}

type notFound struct{}

func (notFound) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
}

func TestCheck_DefaultHostListsAreTheOnesOfTheRealDownloads(t *testing.T) {
	c := upstream.Checker{Client: &http.Client{Transport: notFound{}}} // an allowed URL is asked, and nothing leaves the test
	for _, tc := range []struct {
		hosts upstream.Hosts
		url   string
		ok    bool
	}{
		{upstream.WeightHosts, "https://huggingface.co/a/b/resolve/c/d", true},
		{upstream.WeightHosts, "https://files.pythonhosted.org/packages/ab/x.whl", false},
		{upstream.PackageHosts, "https://files.pythonhosted.org/packages/ab/x.whl", true},
		{upstream.WeightHosts, "https://example.com/x", false},
		{upstream.PackageHosts, "http://huggingface.co/x", false},
	} {
		r := c.Check(context.Background(), upstream.Pin{Label: "t", URL: tc.url, SHA256: pinnedSum, Size: 1, Hosts: tc.hosts, Gated: false})
		if tc.ok {
			require.NotEmpty(t, r.Problems, tc.url)
			assert.Contains(t, r.Problems[0], "gone", tc.url)
			continue
		}
		require.NotEmpty(t, r.Problems, tc.url)
		assert.Contains(t, r.Problems[0], "refused", tc.url)
	}
}

func TestCheck_AGatedFileIsSkippedWithoutATokenAndAskedWithOne(t *testing.T) {
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Length", "9")
	}))
	defer srv.Close()
	p := pin(srv, "/gated", 9, pinnedSum)
	p.Gated = true

	skipped := checker().Check(context.Background(), p)
	assert.Equal(t, upstream.LevelSkipped, skipped.Level)
	assert.Empty(t, skipped.Problems)
	assert.Nil(t, auth.Load(), "nothing was asked")

	c := checker()
	c.HFToken = "hf_secret"
	asked := c.Check(context.Background(), p)
	assert.Empty(t, asked.Problems)
	assert.Equal(t, upstream.LevelSize, asked.Level)
	assert.Empty(t, auth.Load(), "the token goes to huggingface.co only, not to another host")
}

func TestCheck_ARetryRecoversFromAServerError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Length", "3")
	}))
	defer srv.Close()
	c := checker()
	c.Retries = 2

	r := c.Check(context.Background(), pin(srv, "/x", 3, pinnedSum))

	assert.Empty(t, r.Problems)
	assert.EqualValues(t, 2, hits.Load())
}

func TestRun_ChecksEveryPinAndKeepsTheOrder(t *testing.T) {
	var cdn atomic.Int32
	srv := httptest.NewServer(hfLike(10, pinnedSum, &cdn))
	defer srv.Close()
	pins := []upstream.Pin{
		pin(srv, "/a", 10, pinnedSum),
		pin(srv, "/missing", 10, pinnedSum),
		pin(srv, "/c", 11, pinnedSum),
	}

	results := checker().Run(context.Background(), pins)

	require.Len(t, results, 3)
	assert.Empty(t, results[0].Problems)
	assert.NotEmpty(t, results[1].Problems)
	assert.NotEmpty(t, results[2].Problems)
	s := upstream.Summarize(results)
	assert.Equal(t, upstream.Summary{Total: 3, Failed: 2, SHA256: 1}, s)

	var out strings.Builder
	upstream.WriteReport(&out, results, false)
	assert.Contains(t, out.String(), "FAIL test /missing")
	assert.Contains(t, out.String(), "pins: 3  failed: 2")
}
