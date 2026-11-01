package catalog_test

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
)

func TestDefaultIsValidAndPinsTheWindowsArchitecturesAndMacOSArm64(t *testing.T) {
	c := catalog.Default()
	if c.Version != "0.164.0" {
		t.Fatalf("version = %q", c.Version)
	}
	const base = "https://github.com/openai/codex/releases/download/rust-v0.164.0/"
	want := map[[2]string]struct{ url, format, exe string }{
		{"windows", "amd64"}: {base + "codex-app-server-x86_64-pc-windows-msvc.exe.zip", catalog.FormatZip, "codex-app-server-x86_64-pc-windows-msvc.exe"},
		{"windows", "arm64"}: {base + "codex-app-server-aarch64-pc-windows-msvc.exe.zip", catalog.FormatZip, "codex-app-server-aarch64-pc-windows-msvc.exe"},
		{"darwin", "arm64"}:  {base + "codex-app-server-aarch64-apple-darwin.tar.gz", catalog.FormatTarGz, "codex-app-server-aarch64-apple-darwin"},
	}
	assert.Len(t, c.Assets, len(want), "no asset beyond the platforms mam supports")
	for p, w := range want {
		a, ok := c.AssetFor(p[0], p[1])
		if !ok {
			t.Errorf("no asset for %s/%s", p[0], p[1])
			continue
		}
		assert.Equal(t, w.url, a.URL, "%s/%s", p[0], p[1])
		assert.Equal(t, w.format, a.Format, "%s/%s", p[0], p[1])
		assert.Equal(t, w.exe, a.Exe, "%s/%s", p[0], p[1])
	}
	// Platforms without a pinned build: Intel macOS (mam ships Apple Silicon only), Linux, 32-bit Windows, the other OS' arch.
	for _, p := range [][2]string{{"darwin", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"windows", "386"}, {"freebsd", "amd64"}, {"", "arm64"}, {"darwin", ""}} {
		if _, ok := c.AssetFor(p[0], p[1]); ok {
			t.Errorf("%s/%s must have no asset", p[0], p[1])
		}
	}
}

// The macOS asset was fetched from the release and measured by hand: these are the numbers of that archive and of the
// one file inside it (a Mach-O arm64 executable). A new pinned version re-measures them.
func TestDefaultPinsTheMeasuredMacOSAsset(t *testing.T) {
	a, ok := catalog.Default().AssetFor("darwin", "arm64")
	require.True(t, ok)
	assert.Equal(t, "2eb75238651acb7d5c7cbb088b56433c71bbb680189c3bbd50318589858d57b1", a.SHA256)
	assert.Equal(t, int64(78849710), a.Size)
	assert.Equal(t, "45ef99a75f0cc7135765ba9ac8d66a61f5f88d64fd03bf7fce99141d33b4207e", a.ExeSHA256)
	assert.Equal(t, int64(201321648), a.ExeSize)
}

func TestParseRejectsUnpinnedOrMalformedData(t *testing.T) {
	base := catalog.Default()
	good := base.Assets["windows/amd64"]
	mac := base.Assets["darwin/arm64"]
	cases := map[string]func(c *catalog.Catalog, a *catalog.Asset){
		"tag of another version": func(c *catalog.Catalog, _ *catalog.Asset) { c.Tag = "rust-v0.1.0" },
		"another host": func(_ *catalog.Catalog, a *catalog.Asset) {
			a.URL = "https://example.com/openai/codex/releases/download/rust-v0.164.0/x.exe.zip"
		},
		"plain http": func(_ *catalog.Catalog, a *catalog.Asset) { a.URL = strings.Replace(a.URL, "https", "http", 1) },
		"latest instead of tag": func(_ *catalog.Catalog, a *catalog.Asset) {
			a.URL = "https://github.com/openai/codex/releases/latest/download/x.exe.zip"
		},
		"short sha":          func(_ *catalog.Catalog, a *catalog.Asset) { a.SHA256 = "abc" },
		"no size":            func(_ *catalog.Catalog, a *catalog.Asset) { a.Size = 0 },
		"exe with a path":    func(_ *catalog.Catalog, a *catalog.Asset) { a.Exe = "../codex.exe" },
		"exe is a dot-dot":   func(_ *catalog.Catalog, a *catalog.Asset) { a.Exe = ".." },
		"no format":          func(_ *catalog.Catalog, a *catalog.Asset) { a.Format = "" },
		"unknown format":     func(_ *catalog.Catalog, a *catalog.Asset) { a.Format = "7z" },
		"format not the url": func(_ *catalog.Catalog, a *catalog.Asset) { a.Format = catalog.FormatTarGz },
		"windows exe without .exe": func(_ *catalog.Catalog, a *catalog.Asset) {
			a.Exe = "codex-app-server-x86_64-pc-windows-msvc"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base
			c.Assets = map[string]catalog.Asset{"windows/amd64": good}
			a := good
			mutate(&c, &a)
			c.Assets["windows/amd64"] = a
			if err := c.Validate(); err == nil {
				t.Fatal("Validate accepted it")
			}
		})
	}

	t.Run("the macOS asset", func(t *testing.T) {
		withMac := func(mutate func(a *catalog.Asset)) error {
			c := base
			a := mac
			mutate(&a)
			c.Assets = map[string]catalog.Asset{"darwin/arm64": a}
			return c.Validate()
		}
		require.NoError(t, withMac(func(*catalog.Asset) {}))
		assert.Error(t, withMac(func(a *catalog.Asset) { a.Exe += ".exe" }), "a macOS executable has no .exe")
		assert.Error(t, withMac(func(a *catalog.Asset) { a.Format = catalog.FormatZip }), "the format must end the url")
		assert.Error(t, withMac(func(a *catalog.Asset) { a.Exe = "dir/codex" }))
	})

	t.Run("platforms with no Codex build", func(t *testing.T) {
		for _, key := range []string{"amd64", "linux/amd64", "darwin/amd64", "windows/386"} {
			c := base
			c.Assets = map[string]catalog.Asset{key: good}
			assert.Error(t, c.Validate(), key)
		}
	})
}

func TestParseRefusesUnknownFields(t *testing.T) {
	if _, err := catalog.Parse([]byte(`{"version":"1","unknown":true}`)); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}

// TestThirdPartyNoticesNameThePinnedVersionAndAssets keeps THIRD_PARTY_NOTICES.md (generated by
// scripts/third-party-notices.sh) in step with the pinned catalog: bump both together.
func TestThirdPartyNoticesNameThePinnedVersionAndAssets(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "THIRD_PARTY_NOTICES.md"))
	require.NoError(t, err)
	notices := string(raw)
	c := catalog.Default()
	assert.Contains(t, notices, "https://github.com/openai/codex/tree/"+c.Tag)
	assert.Contains(t, notices, "（バージョン "+c.Version+"。")
	for _, a := range c.Assets {
		assert.Contains(t, notices, path.Base(a.URL))
	}
}
