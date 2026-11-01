package engine_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

const exeName = "codex-app-server-test.exe"

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// makeZip is a zip with the given entries (name -> content).
func makeZip(t *testing.T, entries ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(e[1]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type rig struct {
	t       *testing.T
	ins     *engine.Installer
	srv     *httptest.Server
	mu      sync.Mutex
	ranges  []string
	body    []byte // what the server delivers
	onServe func(w http.ResponseWriter, r *http.Request) bool
}

// newRig serves zipBytes and pins asset (the zip's checksum and size, and the exe's content) in the catalog.
func newRig(t *testing.T, zipBytes []byte, exe string) *rig {
	t.Helper()
	r := &rig{t: t, body: zipBytes}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.ranges = append(r.ranges, req.Header.Get("Range"))
		hook := r.onServe
		body := r.body
		r.mu.Unlock()
		if hook != nil && hook(w, req) {
			return
		}
		http.ServeContent(w, req, "codex.zip", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(r.srv.Close)
	dir := t.TempDir()
	r.ins = &engine.Installer{
		Layout:  engine.Layout{Root: filepath.Join(dir, "codex")},
		Version: "9.9.9",
		Asset: catalog.Asset{
			URL: r.srv.URL + "/codex.zip", Format: catalog.FormatZip, SHA256: sum(zipBytes), Size: int64(len(zipBytes)),
			Exe: exeName, ExeSHA256: sum([]byte(exe)), ExeSize: int64(len(exe)),
		},
		Fetch: fetch.Options{Client: r.srv.Client(), CheckURL: func(*url.URL) error { return nil }},
	}
	return r
}

func TestInstallDownloadsVerifiesExtractsAndRecords(t *testing.T) {
	exe := strings.Repeat("MZ-fake-app-server ", 100)
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	var phases []engine.Phase
	var mu sync.Mutex
	err := r.ins.Install(context.Background(), func(p engine.Phase, done, total int64) {
		mu.Lock()
		defer mu.Unlock()
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
		if done > total {
			t.Errorf("progress %d of %d", done, total)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) != 2 || phases[0] != engine.PhaseDownload || phases[1] != engine.PhaseExtract {
		t.Errorf("phases = %v", phases)
	}
	path, ok := r.ins.ExePath()
	if !ok || filepath.Base(path) != exeName {
		t.Fatalf("ExePath = %q, %v", path, ok)
	}
	if got, _ := os.ReadFile(path); string(got) != exe {
		t.Error("the installed file is not the executable of the zip")
	}
	if st := r.ins.Status(); !st.Installed || st.Version != "9.9.9" || st.UpdateAvailable || st.Broken {
		t.Errorf("status = %+v", st)
	}
	for _, gone := range []string{r.ins.Layout.DownloadsDir(), r.ins.Layout.EngineNewDir(), r.ins.Layout.EngineOldDir()} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s is left behind", gone)
		}
	}
}

func TestInstallLeavesNothingWhenTheZipChecksumIsWrong(t *testing.T) {
	exe := "exe"
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	r.ins.Asset.SHA256 = strings.Repeat("0", 64)
	err := r.ins.Install(context.Background(), nil)
	if !errors.Is(err, fetch.ErrChecksum) {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}
	assertNothingInstalled(t, r)
	if _, err := os.Stat(fetch.PartPath(filepath.Join(r.ins.Layout.DownloadsDir(), "codex.zip"))); err == nil {
		t.Error("the part file of a download that did not match is left behind")
	}
}

func TestInstallLeavesNothingWhenTheZipSizeIsWrong(t *testing.T) {
	exe := "exe"
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	r.ins.Asset.Size-- // the server delivers one byte too many
	if err := r.ins.Install(context.Background(), nil); !errors.Is(err, fetch.ErrSize) {
		t.Fatalf("err = %v, want a size mismatch", err)
	}
	assertNothingInstalled(t, r)
}

func TestInstallRefusesAnExecutableThatIsNotThePinnedOne(t *testing.T) {
	// The zip matches its pinned checksum, but the file inside is not the pinned executable.
	r := newRig(t, makeZip(t, [2]string{exeName, "something else entirely"}), "the pinned executable")
	r.ins.Asset.ExeSize = int64(len("something else entirely")) // same size, other content: only the SHA-256 tells
	err := r.ins.Install(context.Background(), nil)
	if !errors.Is(err, exeinstall.ErrMismatch) {
		t.Fatalf("err = %v, want ErrMismatch", err)
	}
	assertNothingInstalled(t, r)
	if _, err := os.Stat(filepath.Join(r.ins.Layout.DownloadsDir(), "codex.zip")); err == nil {
		t.Error("a zip that is not what the catalog describes was kept")
	}
}

func TestInstallRefusesUnsafeArchives(t *testing.T) {
	cases := map[string][][2]string{
		"parent directory": {{"../" + exeName, "exe"}},
		"absolute path":    {{"/" + exeName, "exe"}},
		"windows path":     {{`..\` + exeName, "exe"}},
		"drive":            {{"C:" + exeName, "exe"}},
		"subdirectory":     {{"bin/" + exeName, "exe"}},
		"another name":     {{"other.exe", "exe"}},
		"two entries":      {{exeName, "exe"}, {"extra.dll", "x"}},
		"no entry":         {},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, makeZip(t, entries...), "exe")
			err := r.ins.Install(context.Background(), nil)
			if !errors.Is(err, exeinstall.ErrUnsafeArchive) {
				t.Fatalf("err = %v, want ErrUnsafeArchive", err)
			}
			assertNothingInstalled(t, r)
			if entries := entriesBelow(t, filepath.Dir(r.ins.Layout.Root)); len(entries) > 0 && !onlyCodexDir(entries) {
				t.Errorf("something was written outside the engine directory: %v", entries)
			}
		})
	}
}

func TestInstallResumesAPartFileWithARangeRequest(t *testing.T) {
	exe := strings.Repeat("0123456789abcdef", 64)
	zipBytes := makeZip(t, [2]string{exeName, exe})
	r := newRig(t, zipBytes, exe)
	part := fetch.PartPath(filepath.Join(r.ins.Layout.DownloadsDir(), "codex.zip"))
	half := len(zipBytes) / 2
	if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, zipBytes[:half], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.ins.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ranges) != 1 || r.ranges[0] != "bytes="+strconv.Itoa(half)+"-" {
		t.Errorf("ranges = %q, want one request continuing at %d", r.ranges, half)
	}
	if _, ok := r.ins.ExePath(); !ok {
		t.Error("not installed after the resume")
	}
}

func TestInstallKeepsThePartFileWhenCancelledSoItCanResume(t *testing.T) {
	exe := strings.Repeat("0123456789abcdef", 64)
	zipBytes := makeZip(t, [2]string{exeName, exe})
	r := newRig(t, zipBytes, exe)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.onServe = func(w http.ResponseWriter, req *http.Request) bool {
		w.Header().Set("Content-Length", strconv.Itoa(len(zipBytes)))
		_, _ = w.Write(zipBytes[:len(zipBytes)/2])
		w.(http.Flusher).Flush()
		<-req.Context().Done()
		return true
	}
	// The user cancels once the first bytes are on disk.
	err := r.ins.Install(ctx, func(_ engine.Phase, done, _ int64) {
		if done > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if st, serr := os.Stat(fetch.PartPath(filepath.Join(r.ins.Layout.DownloadsDir(), "codex.zip"))); serr != nil || st.Size() == 0 {
		t.Fatalf("the part file must be kept for the resume: %v", serr)
	}
	if _, ok := r.ins.ExePath(); ok {
		t.Error("a cancelled install must not look installed")
	}
	// The next install continues and succeeds.
	r.mu.Lock()
	r.onServe = nil
	r.mu.Unlock()
	if err := r.ins.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func assertNothingInstalled(t *testing.T, r *rig) {
	t.Helper()
	if _, ok := r.ins.ExePath(); ok {
		t.Error("an install that failed left a usable engine")
	}
	for _, d := range []string{r.ins.Layout.EngineDir(), r.ins.Layout.EngineNewDir(), r.ins.Layout.RecordPath()} {
		if _, err := os.Stat(d); err == nil {
			t.Errorf("%s is left behind", d)
		}
	}
}

func entriesBelow(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err == nil && p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

func onlyCodexDir(entries []string) bool {
	for _, e := range entries {
		if e != "codex" && !strings.HasPrefix(e, "codex"+string(filepath.Separator)) {
			return false
		}
	}
	return true
}
