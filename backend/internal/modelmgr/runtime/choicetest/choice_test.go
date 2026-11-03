// Package choicetest checks that an installer of a runtime with several models (Irodori-TTS) takes the weights of the model in force only, and
// starts the server with that model's folder and settings (spec/architecture/model-manager/local-runtime.md §14.5). It is a package of its own
// because the directory of the installer is at its line budget.
package choicetest_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

var (
	sharedBody = bytes.Repeat([]byte("codec-"), 500)
	smallBody  = bytes.Repeat([]byte("small-"), 800)
	largeBody  = bytes.Repeat([]byte("large-"), 900)
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type host struct {
	*httptest.Server
	mu   sync.Mutex
	asks []string
}

func (h *host) asked() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.asks...)
}

// newInstaller is an installed runtime (its interpreter is a file) with a codec every model shares and a Small (the default) and a Large model.
func newInstaller(t *testing.T) (*rtm.Installer, *host) {
	t.Helper()
	files := map[string][]byte{"/o/r/resolve/" + commit + "/codec.bin": sharedBody, "/o/r/resolve/" + commit + "/small.bin": smallBody, "/o/r/resolve/" + commit + "/large.bin": largeBody}
	h := &host{}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.asks = append(h.asks, r.URL.Path)
		h.mu.Unlock()
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "f", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(h.Close)
	asset := func(name, dest, file string, body []byte, model string) catalog.Resource {
		return catalog.Resource{Name: name, Dest: dest, URL: h.URL + "/o/r/resolve/" + commit + "/" + file, SHA256: sum(body), Size: int64(len(body)), Required: true, Choice: model}
	}
	layout := store.ForRuntime(filepath.Join(t.TempDir(), "localai"), "irodori-tts")
	st, err := store.OpenRuntime(layout)
	require.NoError(t, err)
	i := &rtm.Installer{
		Layout: layout, Store: st,
		Runtime: catalog.Runtime{
			ID: "irodori-tts", Version: "1",
			Launch: catalog.RuntimeLaunch{Python: "python/python.exe", Entry: "launcher.py", Args: []string{"launcher.py", "--host", "127.0.0.1", "--port", "{port}"}, Env: []string{"HF_HUB_OFFLINE=1"}},
		},
		AssetSet: catalog.ResourceSet{
			Runtime: "irodori-tts",
			Assets: []catalog.Resource{
				asset("codec", "models/codec/weights.pth", "codec.bin", sharedBody, ""),
				asset("small", "models/small/model.safetensors", "small.bin", smallBody, "small"),
				asset("large", "models/large/model.safetensors", "large.bin", largeBody, "large"),
			},
			Choices: []choice.Choice{
				{ID: "small", Default: true},
				{ID: "large", Env: []string{"IRODORI_MODEL_PRECISION=bf16"}},
			},
		},
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
			Retry:    fetch.Retry{Attempts: 1},
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
		Getenv:    func(string) string { return "/usr/bin" },
	}
	py := i.PythonPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(py), 0o750))
	require.NoError(t, os.WriteFile(py, []byte("python"), 0o600))
	require.NoError(t, st.Update(func(s *store.RuntimeState) { s.Installed = &store.RuntimeRecord{Version: "1", Variant: "nvidia"} }))
	return i, h
}

func names(as []rtm.AssetStatus) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Name)
	}
	return out
}

func TestTheDefaultModelIsInForceUntilAnotherIsChosen(t *testing.T) {
	i, _ := newInstaller(t)

	assert.Equal(t, []string{"codec", "small"}, names(i.Assets()), "the weights of the default model and the shared ones")
	var small []string
	for _, a := range i.ActiveSet().Assets {
		small = append(small, a.Name)
	}
	assert.Equal(t, []string{"codec", "small"}, small)
	assert.Equal(t, int64(len(sharedBody)+len(smallBody)), i.AssetNeedBytes())

	require.NoError(t, i.Store.Update(func(s *store.RuntimeState) { s.Choice = "large" }))

	assert.Equal(t, []string{"codec", "large"}, names(i.Assets()))
	assert.Equal(t, int64(len(sharedBody)+len(largeBody)), i.AssetNeedBytes())
	require.NoError(t, i.Store.Update(func(s *store.RuntimeState) { s.Choice = "no-such-model" }))
	assert.Equal(t, []string{"codec", "small"}, names(i.Assets()), "a choice the catalog no longer has is the default")
}

func TestEnsureAssetsFetchesOnlyTheWeightsOfTheModelInForce(t *testing.T) {
	i, h := newInstaller(t)

	require.NoError(t, i.EnsureAssets(context.Background(), nil))

	assert.ElementsMatch(t, []string{"/o/r/resolve/" + commit + "/codec.bin", "/o/r/resolve/" + commit + "/small.bin"}, h.asked(), "the other model is not downloaded")
	assert.Empty(t, i.NotReady())
	assert.NoFileExists(t, filepath.Join(i.Layout.EnvDir(), "models", "large", "model.safetensors"))

	require.NoError(t, i.Store.Update(func(s *store.RuntimeState) { s.Choice = "large" }))
	assert.Equal(t, []string{"large"}, names(i.NotReady()), "the shared weights stay verified; only the chosen model's are missing")
	require.NoError(t, i.EnsureAssets(context.Background(), nil))
	assert.Empty(t, i.NotReady())
	assert.Len(t, h.asked(), 3, "the shared file was not fetched again")
}

func TestCommandNamesTheModelInForceAndGivesItsSettings(t *testing.T) {
	i, _ := newInstaller(t)
	env := func() map[string]string {
		spec, err := i.Command(40000)
		require.NoError(t, err)
		out := map[string]string{}
		for _, kv := range spec.Env {
			k, v, _ := strings.Cut(kv, "=")
			out[k] = v
		}
		return out
	}

	got := env()
	assert.Equal(t, "small", got[rtm.ChoiceEnv], "the default model's folder")
	assert.NotContains(t, got, "IRODORI_MODEL_PRECISION", "the settings of the other model are not given")
	assert.Equal(t, "1", got["HF_HUB_OFFLINE"], "the catalog's own settings stay")

	require.NoError(t, i.Store.Update(func(s *store.RuntimeState) { s.Choice = "large" }))
	got = env()
	assert.Equal(t, "large", got[rtm.ChoiceEnv])
	assert.Equal(t, "bf16", got["IRODORI_MODEL_PRECISION"])
}

func TestARuntimeWithoutChoicesIsLeftAsItWas(t *testing.T) {
	i, _ := newInstaller(t)
	i.AssetSet.Choices = nil
	for k := range i.AssetSet.Assets {
		i.AssetSet.Assets[k].Choice = ""
	}

	spec, err := i.Command(40000)

	require.NoError(t, err)
	for _, kv := range spec.Env {
		assert.False(t, strings.HasPrefix(kv, rtm.ChoiceEnv+"="), "no model, no variable")
	}
	assert.Equal(t, []string{"codec", "small", "large"}, names(i.Assets()))
}
