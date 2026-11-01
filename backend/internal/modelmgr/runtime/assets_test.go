package runtime_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// weightsHost is a fake Hugging Face: it serves bodies by path, with Range, and records the requests.
type weightsHost struct {
	*httptest.Server
	mu     sync.Mutex
	files  map[string][]byte
	ranges map[string][]string
	status map[string]int // a path that answers with this status
	// cutOnce: the first request of the path delivers half the body and drops the connection.
	cutOnce map[string]bool
}

func newWeightsHost(t *testing.T) *weightsHost {
	t.Helper()
	h := &weightsHost{files: map[string][]byte{}, ranges: map[string][]string{}, status: map[string]int{}, cutOnce: map[string]bool{}}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.ranges[r.URL.Path] = append(h.ranges[r.URL.Path], r.Header.Get("Range"))
		status, cut := h.status[r.URL.Path], h.cutOnce[r.URL.Path]
		h.cutOnce[r.URL.Path] = false
		body, ok := h.files[r.URL.Path]
		h.mu.Unlock()
		switch {
		case status != 0:
			http.Error(w, "gone", status)
		case !ok:
			http.NotFound(w, r)
		case cut:
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write(body[:len(body)/2])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		default:
			http.ServeContent(w, r, "f", time.Time{}, bytes.NewReader(body))
		}
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *weightsHost) requests(path string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.ranges[path]...)
}

func (h *weightsHost) total() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.ranges {
		n += len(r)
	}
	return n
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func tarGzOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

var (
	weightBody = bytes.Repeat([]byte("weight-"), 4000)
	tinyBody   = []byte(`{"tiny":true}`)
	dictBody   = bytes.Repeat([]byte("dictionary "), 500)
	extraBody  = []byte("extra dependency")
)

// assetRig is an installer for a runtime whose environment is made by hand: a weight, a small config, a zip that holds two files in two
// directories and a tar.gz that holds one.
type assetRig struct {
	*rtm.Installer
	host *weightsHost
}

const (
	weightPath  = "/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/model.bin"
	zipPath     = "/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/data.zip"
	tarPath     = "/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/dict.tar.gz"
	weightDest  = "models/a/model.bin"
	tinyDest    = "models/a/config.json"
	dictDest    = "runtime/dict/sys.dic"
	extraDest   = "models/b/extra.txt"
	tarMemDest  = "runtime/dic/sys.dic"
	tarMemEntry = "dic/sys.dic"
)

func newAssetRig(t *testing.T) *assetRig {
	t.Helper()
	host := newWeightsHost(t)
	zipBody := zipOf(t, map[string][]byte{"pkg/dict.txt": dictBody, "pkg/extra.txt": extraBody})
	tarBody := tarGzOf(t, map[string][]byte{tarMemEntry: dictBody})
	host.files[weightPath], host.files["/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/config.json"] = weightBody, tinyBody
	host.files[zipPath], host.files[tarPath] = zipBody, tarBody

	set := catalog.ResourceSet{Runtime: "gpt-sovits", Assets: []catalog.Resource{
		{Name: "weight", Dest: weightDest, URL: host.URL + weightPath, SHA256: sum(weightBody), Size: int64(len(weightBody)), Required: true},
		{Name: "config", Dest: tinyDest, URL: host.URL + "/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/config.json", SHA256: sum(tinyBody), Size: int64(len(tinyBody)), Required: true},
		{Name: "zip", Format: catalog.FormatZip, URL: host.URL + zipPath, SHA256: sum(zipBody), Size: int64(len(zipBody)), Required: true, Members: []catalog.ResourceMember{
			{Entry: "pkg/dict.txt", Dest: "runtime/dict/dict.txt", Size: int64(len(dictBody)), SHA256: sum(dictBody)},
			{Entry: "pkg/extra.txt", Dest: extraDest, Size: int64(len(extraBody)), SHA256: sum(extraBody)},
		}},
		{Name: "tar", Format: catalog.FormatTarGz, URL: host.URL + tarPath, SHA256: sum(tarBody), Size: int64(len(tarBody)), Required: false, Members: []catalog.ResourceMember{
			{Entry: tarMemEntry, Dest: tarMemDest, Size: int64(len(dictBody)), SHA256: sum(dictBody)},
		}},
	}}
	layout := store.ForRuntime(filepath.Join(t.TempDir(), "localai"), "gpt-sovits")
	st, err := store.OpenRuntime(layout)
	require.NoError(t, err)
	r := &assetRig{host: host, Installer: &rtm.Installer{
		Layout: layout, Store: st, AssetSet: set,
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
	}}
	return r
}

func (r *assetRig) states() map[string]rtm.AssetState {
	out := map[string]rtm.AssetState{}
	for _, s := range r.Assets() {
		out[s.Name] = s.State
	}
	return out
}

func (r *assetRig) put(t *testing.T, rel string, data []byte) string {
	t.Helper()
	p := filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, data, 0o600))
	return p
}

func TestEnsureAssetsDownloadsWhatIsMissingAndVerifiesIt(t *testing.T) {
	r := newAssetRig(t)
	assert.Equal(t, map[string]rtm.AssetState{"weight": rtm.AssetMissing, "config": rtm.AssetMissing, "zip": rtm.AssetMissing, "tar": rtm.AssetMissing}, r.states())
	assert.Equal(t, []string{"weight", "config", "zip"}, func() (names []string) {
		for _, s := range r.NotReady() {
			names = append(names, s.Name)
		}
		return names
	}(), "only what is required keeps the runtime from starting")

	var phases []rtm.Phase
	var last int64
	require.NoError(t, r.EnsureAssets(context.Background(), func(p rtm.Phase, _ string, done, total int64) {
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
		assert.LessOrEqual(t, done, total)
		assert.GreaterOrEqual(t, done, last, "progress never goes back")
		last = done
	}))
	assert.Equal(t, []rtm.Phase{rtm.PhaseWeights}, phases)

	got, err := os.ReadFile(filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(weightDest)))
	require.NoError(t, err)
	assert.Equal(t, weightBody, got)
	for dest, want := range map[string][]byte{tinyDest: tinyBody, "runtime/dict/dict.txt": dictBody, extraDest: extraBody, tarMemDest: dictBody} {
		got, err := os.ReadFile(filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(dest)))
		require.NoError(t, err, dest)
		assert.Equal(t, want, got, dest)
	}
	assert.Equal(t, map[string]rtm.AssetState{"weight": rtm.AssetOK, "config": rtm.AssetOK, "zip": rtm.AssetOK, "tar": rtm.AssetOK}, r.states())
	assert.Empty(t, r.NotReady())
	assert.NoDirExists(t, r.Layout.DownloadsDir(), "an archive is deleted once its files are out")
	assert.NoFileExists(t, filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(weightDest))+".part")
	assert.Len(t, r.Store.Get().Files, 5, "every verified file is recorded")

	// Everything is verified: a second run asks for nothing.
	before := r.host.total()
	require.NoError(t, r.EnsureAssets(context.Background(), nil))
	assert.Equal(t, before, r.host.total())
	assert.Zero(t, r.AssetNeedBytes())
}

func TestEnsureAssetsHashesAFileThatIsThereAndFetchesNothing(t *testing.T) {
	r := newAssetRig(t)
	r.put(t, weightDest, weightBody)
	r.put(t, tinyDest, tinyBody)
	r.put(t, "runtime/dict/dict.txt", dictBody)
	r.put(t, extraDest, extraBody)
	r.put(t, tarMemDest, dictBody)
	assert.Equal(t, map[string]rtm.AssetState{"weight": rtm.AssetUnverified, "config": rtm.AssetUnverified, "zip": rtm.AssetUnverified, "tar": rtm.AssetUnverified}, r.states(),
		"the right size, but nobody has checked the SHA-256 (the package was just extracted)")
	assert.NotEmpty(t, r.NotReady(), "the runtime does not start on files nobody has checked")
	assert.Zero(t, r.AssetNeedBytes(), "nothing is to be downloaded")

	var phases []rtm.Phase
	require.NoError(t, r.EnsureAssets(context.Background(), func(p rtm.Phase, _ string, _, _ int64) {
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
	}))
	assert.Equal(t, []rtm.Phase{rtm.PhaseVerify, rtm.PhaseWeights}, phases, "hashed in place; the last report is the end")
	assert.Zero(t, r.host.total(), "no request: a verified file is not fetched again")
	assert.Empty(t, r.NotReady())
}

func TestEnsureAssetsReplacesAFileThatIsNotThePinnedOne(t *testing.T) {
	r := newAssetRig(t)
	bad := bytes.Repeat([]byte("X"), len(weightBody)) // the same size, other content
	p := r.put(t, weightDest, bad)
	r.put(t, tinyDest, tinyBody)
	require.NoError(t, r.EnsureAssets(context.Background(), nil))
	got, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, weightBody, got, "the damaged file was deleted and fetched again")
	assert.Len(t, r.host.requests(weightPath), 1)
	assert.Empty(t, r.host.requests("/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/config.json"), "the good file stays")
}

func TestAFileThatChangedAfterTheCheckIsUnverifiedAgain(t *testing.T) {
	r := newAssetRig(t)
	require.NoError(t, r.EnsureAssets(context.Background(), nil))
	assert.Empty(t, r.NotReady())

	p := filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(weightDest))
	require.NoError(t, os.WriteFile(p, bytes.Repeat([]byte("Y"), len(weightBody)), 0o600))
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(p, later, later))
	assert.Equal(t, rtm.AssetUnverified, r.states()["weight"])

	require.NoError(t, os.Remove(p))
	assert.Equal(t, rtm.AssetMissing, r.states()["weight"])

	// A file of another size is missing, not "changed".
	require.NoError(t, os.WriteFile(p, []byte("short"), 0o600))
	assert.Equal(t, rtm.AssetMissing, r.states()["weight"])
	require.NoError(t, r.EnsureAssets(context.Background(), nil))
	assert.Equal(t, rtm.AssetOK, r.states()["weight"])
}
