package fetch_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

var payload = []byte(strings.Repeat("model-weights-0123456789", 40000)) // 960 kB

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func source(url string) fetch.Source {
	return fetch.Source{URL: url, SHA256: sum(payload), Size: int64(len(payload))}
}

// anyURL is the check of a test: the servers are plain http on loopback.
func anyURL(*url.URL) error { return nil }

// server serves payload with Range support and records the requests.
type server struct {
	*httptest.Server
	mu       sync.Mutex
	ranges   []string
	auth     []string
	body     []byte
	ignoreRg bool
	cutAfter int // bytes sent before the connection is dropped (0: all)
}

func newServer(t *testing.T, body []byte) *server {
	t.Helper()
	s := &server{body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	body, ignore, cut := s.body, s.ignoreRg, s.cutAfter
	s.mu.Unlock()
	start := 0
	if rg := r.Header.Get("Range"); rg != "" && !ignore {
		n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
		if n >= len(body) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		start = n
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(n)+"-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)-n))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	out := body[start:]
	if cut > 0 && cut < len(out) {
		_, _ = w.Write(out[:cut])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // drops the connection with the body unfinished
	}
	_, _ = w.Write(out)
}

func (s *server) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

// loopbackClient is the client of the downloads with the redirect rules but without the address guard (the
// fake servers are on loopback).
func loopbackClient(authHost string) *http.Client {
	return fetch.WithRedirectRules(&http.Client{}, anyURL, authHost)
}

func plenty(string) (uint64, error) { return 1 << 40, nil }

// opts is a download that does not resume by itself (Retry.Attempts 1): the tests of an interruption look at what it leaves.
func opts(extra ...func(*fetch.Options)) fetch.Options {
	o := fetch.Options{CheckURL: anyURL, FreeBytes: plenty, Client: loopbackClient(""), Retry: fetch.Retry{Attempts: 1}}
	for _, f := range extra {
		f(&o)
	}
	return o
}

func TestDownloadWritesTheVerifiedFileAndReportsProgress(t *testing.T) {
	srv := newServer(t, payload)
	dest := filepath.Join(t.TempDir(), "models", "checkpoints", "m.safetensors")
	var last, total int64
	err := fetch.Download(context.Background(), dest, source(srv.URL), opts(func(o *fetch.Options) {
		o.Progress = func(done, all int64) { last, total = done, all }
	}))
	require.NoError(t, err)
	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.NoFileExists(t, fetch.PartPath(dest))
	assert.Equal(t, int64(len(payload)), last)
	assert.Equal(t, last, total)
}

// A second run finds the file in place and sends no request: the install job may run again after a crash.
func TestDownloadIsIdempotent(t *testing.T) {
	srv := newServer(t, payload)
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), opts()))
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), opts()))
	assert.Len(t, srv.requests(), 1)
}

// An interrupted download leaves the part file; the next run asks for the rest with a Range request and the
// finished file still has the right SHA-256 (the bytes already on disk are hashed again).
func TestDownloadResumesAPartialFile(t *testing.T) {
	srv := newServer(t, payload)
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	srv.cutAfter = 300_000
	err := fetch.Download(context.Background(), dest, source(srv.URL), opts())
	require.Error(t, err)
	assert.NoFileExists(t, dest)
	st, err := os.Stat(fetch.PartPath(dest))
	require.NoError(t, err, "an interruption keeps the part file")
	assert.Positive(t, st.Size())
	assert.Less(t, st.Size(), int64(len(payload)))

	srv.mu.Lock()
	srv.cutAfter = 0
	srv.mu.Unlock()
	var first int64 = -1
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), opts(func(o *fetch.Options) {
		o.Progress = func(done, _ int64) {
			if first < 0 {
				first = done
			}
		}
	})))
	reqs := srv.requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "bytes="+strconv.FormatInt(st.Size(), 10)+"-", reqs[1])
	assert.Greater(t, first, st.Size(), "the progress continues from the bytes in place")
	got, _ := os.ReadFile(dest)
	assert.Equal(t, payload, got)
}

// A server that ignores Range sends the whole file with 200: the part file is overwritten from the start.
func TestDownloadRestartsWhenTheServerIgnoresRange(t *testing.T) {
	srv := newServer(t, payload)
	srv.ignoreRg = true
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	require.NoError(t, os.WriteFile(fetch.PartPath(dest), payload[:1000], 0o600))
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), opts()))
	got, _ := os.ReadFile(dest)
	assert.Equal(t, payload, got)
}

// A part file that cannot be continued (416, or longer than the file) is thrown away and the file is fetched anew.
func TestDownloadDropsAnUnusablePartFile(t *testing.T) {
	srv := newServer(t, payload)
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	require.NoError(t, os.WriteFile(fetch.PartPath(dest), append(append([]byte{}, payload...), 'x'), 0o600))
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), opts()))
	got, _ := os.ReadFile(dest)
	assert.Equal(t, payload, got)
	assert.Equal(t, []string{""}, srv.requests(), "the oversized part file was dropped before the request")

	// 416 for a part file of the right size that the server will not continue: the whole file again.
	dest2 := filepath.Join(t.TempDir(), "n.safetensors")
	srv.body = payload
	require.NoError(t, os.WriteFile(fetch.PartPath(dest2), payload[:len(payload)-1], 0o600))
	srv.mu.Lock()
	srv.ranges = nil
	srv.mu.Unlock()
	require.NoError(t, fetch.Download(context.Background(), dest2, source(srv.URL), opts()))
	got, _ = os.ReadFile(dest2)
	assert.Equal(t, payload, got)
}

func TestDownloadRefusesAndCleansUpAChecksumMismatch(t *testing.T) {
	bad := append([]byte{}, payload...)
	bad[10] ^= 0xff
	srv := newServer(t, bad)
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	err := fetch.Download(context.Background(), dest, source(srv.URL), opts())
	require.ErrorIs(t, err, fetch.ErrChecksum)
	assert.NoFileExists(t, dest, "a file that failed the check never appears at its destination")
	assert.NoFileExists(t, fetch.PartPath(dest), "and its part file is removed: resuming it could not fix it")
}

func TestDownloadRefusesAWrongSize(t *testing.T) {
	for name, body := range map[string][]byte{"longer": append(append([]byte{}, payload...), "extra"...), "shorter": payload[:len(payload)-5]} {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, body)
			dest := filepath.Join(t.TempDir(), "m.safetensors")
			err := fetch.Download(context.Background(), dest, source(srv.URL), opts())
			require.Error(t, err)
			assert.NoFileExists(t, dest)
			if name == "longer" {
				require.ErrorIs(t, err, fetch.ErrSize)
				assert.NoFileExists(t, fetch.PartPath(dest))
			} else {
				assert.FileExists(t, fetch.PartPath(dest), "a short body is an interruption: it resumes")
			}
		})
	}
}

func TestDownloadStartsNothingWithoutFreeSpace(t *testing.T) {
	srv := newServer(t, payload)
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	err := fetch.Download(context.Background(), dest, source(srv.URL), opts(func(o *fetch.Options) {
		o.FreeBytes = func(string) (uint64, error) { return uint64(len(payload)) - 1, nil }
	}))
	var ns *fetch.NoSpaceError
	require.ErrorAs(t, err, &ns)
	assert.Equal(t, uint64(len(payload)), ns.Need)
	assert.Empty(t, srv.requests(), "no request is sent")
	assert.NoFileExists(t, fetch.PartPath(dest))

	// Only the missing part counts for a resumed file.
	require.NoError(t, os.WriteFile(fetch.PartPath(dest), payload[:900_000], 0o600))
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), opts(func(o *fetch.Options) {
		o.FreeBytes = func(string) (uint64, error) { return uint64(len(payload)) - 900_000, nil }
	})))
}

func TestDownloadFailsDefinitivelyOn4xxAndKeepsPartFilesOn5xx(t *testing.T) {
	for status, definitive := range map[int]bool{http.StatusNotFound: true, http.StatusUnauthorized: true, http.StatusInternalServerError: false, http.StatusTooManyRequests: false} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			defer srv.Close()
			dest := filepath.Join(t.TempDir(), "m.safetensors")
			require.NoError(t, os.WriteFile(fetch.PartPath(dest), payload[:100], 0o600))
			err := fetch.Download(context.Background(), dest, source(srv.URL), opts())
			var he *fetch.HTTPError
			require.ErrorAs(t, err, &he)
			assert.Equal(t, definitive, he.Definitive())
			if definitive {
				assert.NoFileExists(t, fetch.PartPath(dest))
			} else {
				assert.FileExists(t, fetch.PartPath(dest))
			}
		})
	}
}

func TestDownloadStopsOnCancelAndKeepsThePartFile(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload[:200_000])
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	errc := make(chan error, 1)
	go func() { errc <- fetch.Download(ctx, dest, source(srv.URL), opts()) }()
	<-started
	// The server flushed, but the client may not have written a byte yet: cancel only once it has.
	require.Eventually(t, func() bool {
		st, err := os.Stat(fetch.PartPath(dest))
		return err == nil && st.Size() > 0
	}, 10*time.Second, 5*time.Millisecond, "the client wrote nothing to the part file")
	cancel()
	select {
	case err := <-errc:
		require.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the download did not stop")
	}
	assert.FileExists(t, fetch.PartPath(dest))
}

func TestDownloadAbortsAStalledTransfer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload[:1000])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	err := fetch.Download(context.Background(), dest, source(srv.URL), opts(func(o *fetch.Options) { o.IdleTimeout = 200 * time.Millisecond }))
	require.Error(t, err)
	assert.FileExists(t, fetch.PartPath(dest))
}

// The Hugging Face token goes to the auth host only; a redirect to another host (the CDN) carries no
// Authorization header, and a redirect to a host that is not allowed is refused.
