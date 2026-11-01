package fetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

func TestTokenIsSentToTheAuthHostOnly(t *testing.T) {
	cdn := newServer(t, payload)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL+"/file", http.StatusFound)
		origins <- r.Header.Get("Authorization")
	}))
	defer origin.Close()
	originHost := mustHost(t, origin.URL)

	run := func(authHost string) error {
		dest := filepath.Join(t.TempDir(), "m.safetensors")
		return fetch.Download(context.Background(), dest, source(origin.URL+"/f"), fetch.Options{
			CheckURL: anyURL, FreeBytes: plenty, AuthHost: authHost, Token: func() string { return "hf_secret" },
			Client: loopbackClient(authHost),
		})
	}
	require.NoError(t, run(originHost))
	assert.Equal(t, "Bearer hf_secret", <-origins, "the auth host gets the token")
	assert.Equal(t, []string{""}, cdn.authHeaders(), "the CDN host never does")

	require.NoError(t, run("huggingface.co"))
	assert.Equal(t, "", <-origins, "another host gets no token at all")
}

var origins = make(chan string, 4)

func (s *server) authHeaders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...)
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Hostname()
}

func TestAllowedURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://huggingface.co/Comfy-Org/x/resolve/abc/f.safetensors":   true,
		"https://cdn-lfs.huggingface.co/repos/a":                         true,
		"https://us.aws.cdn.hf.co/xet-bridge-us/abc":                     true,
		"https://github.com/Comfy-Org/ComfyUI/releases/download/v1/a":    true,
		"https://release-assets.githubusercontent.com/github-production": true,
		"http://huggingface.co/x":                                        false,
		"https://example.com/x":                                          false,
		"https://evilhuggingface.co/x":                                   false,
		"https://huggingface.co.evil.com/x":                              false,
		"https://127.0.0.1/x":                                            false,
		"https://localhost/x":                                            false,
	} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, ok, fetch.AllowedURL(u) == nil, raw)
	}
}

// The parts of an assembled runtime (MuseTalk) come from the hosts of AllowedURL and the Python package hosts, and from no other.
func TestPackageURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://huggingface.co/o/m/resolve/abc/f.7z":                                             true,
		"https://github.com/TMElyralab/MuseTalk/archive/abc.zip":                                  true,
		"https://codeload.github.com/TMElyralab/MuseTalk/zip/abc":                                 true,
		"https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip":                true,
		"https://files.pythonhosted.org/packages/ab/cd/numpy-1.23.5-cp310-cp310-win_amd64.whl":    true,
		"https://download.pytorch.org/whl/cu118/torch-2.0.1%2Bcu118-cp310-cp310-win_amd64.whl":    true,
		"https://download-r2.pytorch.org/whl/cu118/torch-2.0.1%2Bcu118-cp310-cp310-win_amd64.whl": true,
		"https://download.openmmlab.com/mmcv/dist/cu118/torch2.0.0/mmcv-2.0.1.whl":                true,
		"http://files.pythonhosted.org/packages/x.whl":                                            false,
		"https://pypi.org/simple/numpy/":                                                          false,
		"https://evilpytorch.org/x.whl":                                                           false,
		"https://pytorch.org.evil.com/x.whl":                                                      false,
		"https://127.0.0.1/x":                                                                     false,
	} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, ok, fetch.PackageURL(u) == nil, raw)
		if ok && !strings.Contains(raw, "huggingface") && !strings.Contains(raw, "github") {
			assert.Error(t, fetch.AllowedURL(u), "the weights and the engine never come from a package host: "+raw)
		}
	}
}

// The Hugging Face import never leaves Hugging Face: GitHub (the engine archive) is allowed for the engine download only.
func TestHFAllowedURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://huggingface.co/o/m/resolve/abc/f.safetensors": true,
		"https://cdn-lfs.huggingface.co/repos/a":               true,
		"https://us.aws.cdn.hf.co/xet-bridge-us/abc":           true,
		"https://github.com/Comfy-Org/ComfyUI/releases/a":      false,
		"https://release-assets.githubusercontent.com/x":       false,
		"http://huggingface.co/x":                              false,
		"https://evilhuggingface.co/x":                         false,
		"https://huggingface.co.evil.com/x":                    false,
		"https://127.0.0.1/x":                                  false,
	} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, ok, fetch.HFAllowedURL(u) == nil, raw)
	}
}

func TestVerified(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	ok, err := fetch.Verified(f, source(""))
	require.NoError(t, err)
	assert.False(t, ok, "missing")
	require.NoError(t, os.WriteFile(f, payload, 0o600))
	ok, _ = fetch.Verified(f, source(""))
	assert.True(t, ok)
	require.NoError(t, os.WriteFile(f, payload[:len(payload)-1], 0o600))
	ok, _ = fetch.Verified(f, source(""))
	assert.False(t, ok, "short")
	bad := append([]byte{}, payload...)
	bad[0] ^= 1
	require.NoError(t, os.WriteFile(f, bad, 0o600))
	ok, _ = fetch.Verified(f, source(""))
	assert.False(t, ok, "same size, other content")
}
