// Package hftest is a fake Hugging Face for the tests of the model manager's import: the search, revision, tree (with
// pages), paths-info and resolve endpoints, with the answers the real Hub gives (a 302 to a CDN with Range support, 401
// with x-error-code GatedRepo, 307 for a renamed repository, 429 with a rate-limit header). It records the Authorization
// header of every request by host, so that a test can show the token goes to the API host only. Not for production code.
package hftest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// File is a file of a fake repository.
type File struct {
	Path string
	Body []byte
	// NoLFS makes the file a plain Git file (no lfs object: its SHA-256 is unknown).
	NoLFS bool
	// Security is the securityFileStatus.status paths-info answers ("" = safe).
	Security string
	// Served, when set, is what the CDN delivers instead of Body (the Hub's pins describe Body): a corrupted or swapped file.
	Served []byte
}

// Repo is a fake repository. Its Commit is the only revision besides "main".
type Repo struct {
	ID     string
	Commit string
	Files  []File
	// Gated needs Token and Granted to download; the metadata stays public.
	Gated bool
	// NFAA tags the repository not-for-all-audiences.
	NFAA bool
	// License is cardData.license ("" for none); LicenseName and LicenseLink are the other card fields.
	License, LicenseName, LicenseLink string
	Downloads, Likes                  int64
	PipelineTag                       string
	Disabled                          bool
}

// Server is the fake. The CDN lives on the same server under /cdn/, addressed by the host alias CDNHost (a different
// host name for the same listener), so that a redirect leaves the host the token was sent to.
type Server struct {
	*httptest.Server
	// Token is the bearer token that is accepted (default "hf_testtoken").
	Token string
	// Granted is whether the token has access to the gated repositories.
	Granted bool
	// PageSize is how many tree entries a page holds (default 2).
	PageSize int

	mu      sync.Mutex
	repos   map[string]Repo
	renames map[string]string
	limited int
	reqs    []Request
}

// Request is a request the fake saw.
type Request struct {
	Method, Path, Host, Auth, Range string
	// Query is the raw query string.
	Query string
}

// New starts the fake with the repositories.
func New(t *testing.T, repos ...Repo) *Server {
	t.Helper()
	s := &Server{Token: "hf_testtoken", PageSize: 2, repos: map[string]Repo{}, renames: map[string]string{}}
	for _, r := range repos {
		s.repos[r.ID] = r
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// CDNHost is the other host name of the listener ("localhost" for the "127.0.0.1" the server is on).
func (s *Server) CDNHost() string {
	u, _ := url.Parse(s.URL)
	return "localhost:" + u.Port()
}

// Add serves another repository (or replaces the one of that ID).
func (s *Server) Add(r Repo) {
	s.mu.Lock()
	s.repos[r.ID] = r
	s.mu.Unlock()
}

// Rename makes old a repository that moved to to: its API calls answer 307.
func (s *Server) Rename(old, to string) {
	s.mu.Lock()
	s.renames[old] = to
	s.mu.Unlock()
}

// RateLimit makes the next n requests answer 429 (Retry-After: 0).
func (s *Server) RateLimit(n int) {
	s.mu.Lock()
	s.limited = n
	s.mu.Unlock()
}

// Requests are the requests seen so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Count is how many requests had a path with the prefix.
func (s *Server) Count(prefix string) int {
	n := 0
	for _, r := range s.Requests() {
		if strings.HasPrefix(r.Path, prefix) {
			n++
		}
	}
	return n
}

// Safetensors builds a .safetensors file with the tensors named names (one F16 value each), the metadata, and padding bytes of data
// after them.
func Safetensors(names []string, metadata map[string]string, pad int) []byte {
	hdr := map[string]any{}
	off := 0
	for _, n := range names {
		hdr[n] = map[string]any{"dtype": "F16", "shape": []int{1}, "data_offsets": []int{off, off + 2}}
		off += 2
	}
	if metadata != nil {
		hdr["__metadata__"] = metadata
	}
	raw, _ := json.Marshal(hdr)
	out := make([]byte, 8, 8+len(raw)+off+pad)
	binary.LittleEndian.PutUint64(out, uint64(len(raw)))
	out = append(out, raw...)
	out = append(out, make([]byte, off+pad)...)
	return out
}

// SHA is the SHA-256 of a body as the Hub's lfs.oid.
func SHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.reqs = append(s.reqs, Request{Method: r.Method, Path: r.URL.Path, Host: r.Host, Auth: r.Header.Get("Authorization"), Range: r.Header.Get("Range"), Query: r.URL.RawQuery})
	limited := s.limited > 0
	if limited {
		s.limited--
	}
	s.mu.Unlock()
	if limited {
		w.Header().Set("Retry-After", "0")
		http.Error(w, "slow down", http.StatusTooManyRequests)
		return
	}
	switch {
	case r.URL.Path == "/api/models":
		s.search(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/models/"):
		s.api(w, r)
	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		s.cdn(w, r)
	default:
		s.resolve(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+s.Token
}
