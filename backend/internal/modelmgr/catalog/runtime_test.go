package catalog_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

func TestGPTSoVITSRuntimeIsPinnedToTheOfficialPackage(t *testing.T) {
	c := catalog.Default()
	rt, ok := c.Runtime("gpt-sovits")
	require.True(t, ok)
	assert.Equal(t, catalog.PurposeTTS, rt.Purpose)
	assert.Equal(t, "https://github.com/RVC-Boss/GPT-SoVITS/tree/20250606v2pro", rt.SourceURL)
	assert.Equal(t, "mit", rt.License.ID)
	assert.Equal(t, "GPT-SoVITS-v2pro-20250604", rt.ArchiveRoot)
	assert.Equal(t, []catalog.Variant{catalog.VariantNVIDIA, catalog.VariantNVIDIA50}, rt.VariantList())

	const pin = "https://huggingface.co/lj1995/GPT-SoVITS-windows-package/resolve/c8b61d4f627d1f02e74c1ba158a4844051d2289f/"
	std, nv50 := rt.Variants[catalog.VariantNVIDIA], rt.Variants[catalog.VariantNVIDIA50]
	assert.Equal(t, pin+"GPT-SoVITS-v2pro-20250604.7z", std.URL)
	assert.Equal(t, "bd60d0796553ff05d8568136e199c13e0dc22ebe2ed24273134e34ed6f215cd6", std.SHA256)
	assert.Equal(t, int64(8185086602), std.Size)
	assert.Equal(t, pin+"GPT-SoVITS-v2pro-20250604-nvidia50.7z", nv50.URL)
	assert.Equal(t, "97b4edcd451c42357db7e26e6c1c877ca5d85144fe97beaff6d7005d35bee008", nv50.SHA256)
	assert.Equal(t, int64(8835144925), nv50.Size)
	assert.InDelta(t, 12.0, nv50.MinComputeCap, 0)
	assert.Zero(t, std.MinComputeCap)

	// The server listens on the loopback and the port mam chose; it is not allowed to fetch anything by itself.
	assert.Contains(t, rt.Launch.Args, "127.0.0.1")
	assert.Contains(t, rt.Launch.Args, "{port}")
	assert.Contains(t, rt.Launch.Env, "HF_HUB_OFFLINE=1")
	assert.Contains(t, rt.Launch.Env, "TRANSFORMERS_OFFLINE=1")
	assert.False(t, rt.License.CommercialUse && rt.License.CommercialCondition == "", "what is not clear about commercial use is said")

	// Why a machine is refused is data of the catalog: macOS has its own sentence.
	assert.Contains(t, rt.Support.OSReasons["darwin"], "macOS では GPT-SoVITS の自動導入に対応していません")
	assert.Contains(t, rt.Support.Fallback, "ElevenLabs")
}

func TestCatalogWithoutRuntimesStillLoads(t *testing.T) {
	fsys := fstest.MapFS{
		"data/engine.json":   {Data: []byte(`{"version":"v1.0.0"}`)},
		"data/models/x.json": {Data: []byte(`{"id":"x"}`)},
	}
	c, err := catalog.Load(fsys)
	require.NoError(t, err)
	assert.Empty(t, c.Runtimes)
	_, ok := c.Runtime("gpt-sovits")
	assert.False(t, ok)
}

func TestLoadRefusesUnknownFieldsOfARuntime(t *testing.T) {
	fsys := fstest.MapFS{
		"data/engine.json":     {Data: []byte(`{"version":"v1.0.0"}`)},
		"data/models/x.json":   {Data: []byte(`{"id":"x"}`)},
		"data/runtimes/y.json": {Data: []byte(`{"id":"y","bogus":1}`)},
	}
	_, err := catalog.Load(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func runtimeOf(c *catalog.Catalog) *catalog.Runtime { return &c.Runtimes[0] }

func TestValidateRefusesARuntimeThatIsNotPinnedOrNotLoopbackOnly(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(c *catalog.Catalog)
		want string
	}{
		"branch instead of a commit": {func(c *catalog.Catalog) {
			v := runtimeOf(c).Variants[catalog.VariantNVIDIA]
			v.URL = strings.Replace(v.URL, "c8b61d4f627d1f02e74c1ba158a4844051d2289f", "main", 1)
			runtimeOf(c).Variants[catalog.VariantNVIDIA] = v
		}, "40-hex commit"},
		"plain http": {func(c *catalog.Catalog) {
			v := runtimeOf(c).Variants[catalog.VariantNVIDIA]
			v.URL = strings.Replace(v.URL, "https", "http", 1)
			runtimeOf(c).Variants[catalog.VariantNVIDIA] = v
		}, "40-hex commit"},
		"another host": {func(c *catalog.Catalog) {
			v := runtimeOf(c).Variants[catalog.VariantNVIDIA]
			v.URL = strings.Replace(v.URL, "huggingface.co", "example.com", 1)
			runtimeOf(c).Variants[catalog.VariantNVIDIA] = v
		}, "40-hex commit"},
		"no sha256": {func(c *catalog.Catalog) {
			v := runtimeOf(c).Variants[catalog.VariantNVIDIA]
			v.SHA256 = ""
			runtimeOf(c).Variants[catalog.VariantNVIDIA] = v
		}, "sha256"},
		"extracted smaller than the archive": {func(c *catalog.Catalog) {
			v := runtimeOf(c).Variants[catalog.VariantNVIDIA]
			v.ExtractedSize = 1
			runtimeOf(c).Variants[catalog.VariantNVIDIA] = v
		}, "extracted_size (larger)"},
		"no base build": {func(c *catalog.Catalog) {
			v := runtimeOf(c).Variants[catalog.VariantNVIDIA]
			v.MinComputeCap = 7.5
			runtimeOf(c).Variants[catalog.VariantNVIDIA] = v
		}, "exactly one variant"},
		"no variant": {func(c *catalog.Catalog) { runtimeOf(c).Variants = nil }, "at least one variant"},
		"listens on every interface": {func(c *catalog.Catalog) {
			a := runtimeOf(c).Launch.Args
			for i := range a {
				if a[i] == "127.0.0.1" {
					a[i] = "0.0.0.0"
				}
			}
		}, "never on every interface"},
		"no port placeholder": {func(c *catalog.Catalog) {
			a := runtimeOf(c).Launch.Args
			for i := range a {
				if a[i] == "{port}" {
					a[i] = "9880"
				}
			}
		}, "never on every interface"},
		"no offline environment":        {func(c *catalog.Catalog) { runtimeOf(c).Launch.Env = nil }, "offline env"},
		"a broken environment entry":    {func(c *catalog.Catalog) { runtimeOf(c).Launch.Env = []string{"nonsense"} }, "NAME=value"},
		"an interpreter outside":        {func(c *catalog.Catalog) { runtimeOf(c).Launch.Python = "../python.exe" }, "relative slash path"},
		"an entry that args do not run": {func(c *catalog.Catalog) { runtimeOf(c).Launch.Entry = "other.py" }, "launch entry"},
		"a source that is a branch":     {func(c *catalog.Catalog) { runtimeOf(c).SourceURL = "https://example.com/x" }, "source_url"},
		"no VRAM guideline source":      {func(c *catalog.Catalog) { runtimeOf(c).VRAM.Source = "" }, "source of the guideline"},
		"a purpose that is unknown":     {func(c *catalog.Catalog) { runtimeOf(c).Purpose = "image" }, "purpose"},
		"no fallback":                   {func(c *catalog.Catalog) { runtimeOf(c).Support.Fallback = "" }, "the fallback"},
		"no reason for another OS":      {func(c *catalog.Catalog) { delete(runtimeOf(c).Support.OSReasons, "default") }, "os_reasons.default"},
		"a reason for the supported OS": {func(c *catalog.Catalog) { runtimeOf(c).Support.OSReasons["windows"] = "x" }, "supported OS"},
		"missing license summary":       {func(c *catalog.Catalog) { runtimeOf(c).License.Summary = "" }, "license needs"},
		"an id that is not a slug":      {func(c *catalog.Catalog) { runtimeOf(c).ID = "../x" }, "id (slug)"},
		"duplicate id": {func(c *catalog.Catalog) {
			c.Runtimes = append(c.Runtimes, c.Runtimes[0])
		}, "duplicate id"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}
