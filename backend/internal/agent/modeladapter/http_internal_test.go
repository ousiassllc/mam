package modeladapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestOwnsURLOnlyAcceptsTheVendorsHosts(t *testing.T) {
	tests := []struct {
		base, target string
		want         bool
	}{
		{"https://api.bfl.ai", "https://api.bfl.ai/v1/get_result?id=1", true},
		{"https://api.bfl.ai", "https://api.eu.bfl.ai/v1/get_result?id=1", true},
		{"https://api.bfl.ai", "https://API.US.BFL.AI/x", true},
		{"https://api.bfl.ai", "https://evil.example/v1/get_result", false},
		{"https://api.bfl.ai", "https://bfl.ai.evil.example/", false},
		{"https://api.bfl.ai", "https://evilbfl.ai/", false},
		{"https://api.bfl.ai", "http://api.bfl.ai/x", false},
		{"https://api.bfl.ai", "https://api.bfl.ai:8443/x", false},
		{"https://api.bfl.ai", "https://user@api.bfl.ai/x", false},
		{"https://api.bfl.ai", "https://127.0.0.1/x", false},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/poll", true},
		{"http://127.0.0.1:8080", "http://127.0.0.2:8080/poll", false},
		{"http://127.0.0.1:8080", "http://127.0.0.1:9090/poll", false},
		{"https://bfl.ai", "https://bfl.ai/x", true},
		{"https://bfl.ai", "https://eu.bfl.ai/x", true},
		{"", "https://api.bfl.ai/x", false},
	}
	for _, tt := range tests {
		if got := (api{baseURL: tt.base}).ownsURL(tt.target); got != tt.want {
			t.Errorf("ownsURL(base %q, %q) = %v, want %v", tt.base, tt.target, got, tt.want)
		}
	}
}

func downloadAPI(srv *httptest.Server, staging string) api {
	return api{
		vendor: VendorSeedance, fetcher: srv.Client(), localDownloads: true,
		stagingDir: staging, pollEvery: time.Millisecond,
	}
}

func stagedFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestDownloadStreamsALargeBodyToDiskWithoutBufferingIt(t *testing.T) {
	const size = 120 << 20
	chunk := make([]byte, 1<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		for range size / len(chunk) {
			_, _ = w.Write(chunk)
		}
	}))
	defer srv.Close()
	staging := t.TempDir()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	blob, mime, err := downloadAPI(srv, staging).download(context.Background(), srv.URL)

	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer blob.Discard()
	if blob.Size() != size || mime != "video/mp4" {
		t.Fatalf("size %d mime %q", blob.Size(), mime)
	}
	if info, err := os.Stat(stagedFiles(t, staging)[0]); err != nil || info.Size() != size {
		t.Fatalf("staged file: %v %v", info, err)
	}
	// io.ReadAll would allocate at least the file size (twice, with its growing buffer).
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/10 {
		t.Fatalf("the download allocated %d bytes for a %d byte file", allocated, size)
	}
}

func TestDownloadLeavesNoStagedFileWhenTheBodyIsCutOrTooLarge(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"cut": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte("only a part"))
		},
		"too large": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Transfer-Encoding", "chunked")
			_, _ = io.WriteString(w, "0123456789")
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "0123456789")
		},
		"too large by header": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "20")
			_, _ = io.WriteString(w, "01234567890123456789")
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			staging := t.TempDir()
			a := downloadAPI(srv, staging)
			a.maxDownload = 15

			_, _, err := a.download(context.Background(), srv.URL)

			if err == nil {
				t.Fatal("the download must fail")
			}
			if left := stagedFiles(t, staging); len(left) != 0 {
				t.Fatalf("staged files left: %v", left)
			}
		})
	}
}

func TestBlobStoreAsMovesTheStagedFileAndOnlyTheCompleteFileAppears(t *testing.T) {
	staging, dst := t.TempDir(), filepath.Join(t.TempDir(), "asset.mp4")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "MP4DATA") }))
	defer srv.Close()
	blob, _, err := downloadAPI(srv, staging).download(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("the asset must not exist before it is stored")
	}

	if err := blob.StoreAs(dst, 0o600); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(dst); string(got) != "MP4DATA" {
		t.Fatalf("asset content %q", got)
	}
	if left := stagedFiles(t, staging); len(left) != 0 {
		t.Fatalf("staged files left: %v", left)
	}
}

func TestBlobStoreAsDiscardsTheStagedFileWhenStoringFails(t *testing.T) {
	staging := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "MP4DATA") }))
	defer srv.Close()
	blob, _, err := downloadAPI(srv, staging).download(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	err = blob.StoreAs(filepath.Join(t.TempDir(), "missing-dir", "asset.mp4"), 0o600)

	if err == nil {
		t.Fatal("storing into a missing directory must fail")
	}
	if left := stagedFiles(t, staging); len(left) != 0 {
		t.Fatalf("staged files left: %v", left)
	}
}
