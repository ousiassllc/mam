package platform_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
)

func blackwellOnly() *platform.Requires {
	return &platform.Requires{GPUArch: []string{platform.ArchBlackwell}, Reason: "FP4 の演算が要ります。", Source: "docs 2026-11-03"}
}

func noMac() *platform.Unsupported {
	return &platform.Unsupported{OS: []string{platform.OSDarwin}, Reason: "fp8 を MPS に載せられません。", Source: "issue 2026-11-03"}
}

// A PC inside the columns, a model without columns, and what is not known get no warning; only a host that is known to be outside does. An
// install is never refused, and the words say so.
func TestWarning(t *testing.T) {
	for name, tc := range map[string]struct {
		r    *platform.Requires
		u    *platform.Unsupported
		host platform.Host
		want []string // substrings of the warning; nil: none
	}{
		"no columns":                  {nil, nil, platform.Host{OS: "windows", GPUArch: platform.ArchAda}, nil},
		"Blackwell is inside":         {blackwellOnly(), nil, platform.Host{OS: "windows", GPUArch: platform.ArchBlackwell}, nil},
		"Ada is outside Blackwell":    {blackwellOnly(), nil, platform.Host{OS: "windows", GPUArch: platform.ArchAda}, []string{"NVIDIA Ada Lovelace", "対象: NVIDIA Blackwell", "FP4 の演算が要ります。", "導入はできます"}},
		"AMD is outside Blackwell":    {blackwellOnly(), nil, platform.Host{OS: "windows", GPUArch: platform.ArchAMD}, []string{"AMD Radeon", "対象外"}},
		"a Mac is outside Blackwell":  {blackwellOnly(), nil, platform.Host{OS: "darwin", GPUArch: platform.ArchApple}, []string{"Mac の GPU", "対象外"}},
		"unknown generation is quiet": {blackwellOnly(), nil, platform.Host{OS: "windows"}, nil},
		"no GPU is quiet":             {blackwellOnly(), nil, platform.Host{}, nil},
		"Windows is not unsupported":  {nil, noMac(), platform.Host{OS: "windows", GPUArch: platform.ArchAda}, nil},
		"macOS is unsupported":        {nil, noMac(), platform.Host{OS: "darwin"}, []string{"macOS", "動かない可能性が高い", "fp8 を MPS に載せられません。"}},
		"unknown OS is quiet":         {nil, noMac(), platform.Host{}, nil},
		"both say so":                 {blackwellOnly(), noMac(), platform.Host{OS: "darwin", GPUArch: platform.ArchApple}, []string{"macOS", "Mac の GPU"}},
		"OS requirement":              {&platform.Requires{OS: []string{platform.OSWindows}, Reason: "r", Source: "s 2026-11-03"}, nil, platform.Host{OS: "linux"}, []string{"Linux", "対象: Windows"}},
	} {
		got := platform.Warning(tc.r, tc.u, tc.host)
		if tc.want == nil {
			assert.Empty(t, got, name)
			continue
		}
		for _, w := range tc.want {
			assert.Contains(t, got, w, name)
		}
	}
}

// Validate wants known values, a reason and a source that says when it was read, and refuses a column that says nothing.
func TestValidate(t *testing.T) {
	assert.Empty(t, platform.Validate("m", blackwellOnly(), noMac()))
	assert.Empty(t, platform.Validate("m", nil, nil))

	has := func(errs []error, part string) bool {
		for _, e := range errs {
			if strings.Contains(e.Error(), part) {
				return true
			}
		}
		return false
	}
	errs := platform.Validate("m", &platform.Requires{GPUArch: []string{"blackwel"}, OS: []string{"os2", "os2"}}, &platform.Unsupported{})
	assert.True(t, has(errs, `"blackwel" is not a known value`), "%v", errs)
	assert.True(t, has(errs, `"os2" is not a known value`), "%v", errs)
	assert.True(t, has(errs, "the reason is required"), "%v", errs)
	assert.True(t, has(errs, "source must say"), "%v", errs)
	assert.True(t, has(errs, "unsupported names no os"), "%v", errs)
	assert.True(t, has(platform.Validate("m", &platform.Requires{Reason: "r", Source: "s 2026-11-03"}, nil), "neither gpu_arch nor os"))
	assert.True(t, has(platform.Validate("m", &platform.Requires{GPUArch: []string{"ada", "ada"}, Reason: "r", Source: "s 2026-11-03"}, nil), "listed twice"))
	assert.True(t, has(platform.Validate("m", nil, &platform.Unsupported{OS: []string{"darwin"}, Reason: "r", Source: "s 2026-13-45"}), "source must say"), "not a real date")
}

// The catalog's columns (Issue #979): NVFP4 LTX is Blackwell only (Lightricks' docs, ComfyUI's supports_nvfp4_compute), and every model that
// loads an fp8 file does not run on macOS (MPS cannot hold Float8_e4m3fn). The columns come with a reason and the date the source was read, and
// the models that have none (SDXL, the INT8 / bf16 LTX) stay without: nothing is filled in that no primary source says.
func TestCatalogColumns(t *testing.T) {
	c := catalog.Default()
	nv, ok := c.Model("ltx-2.5-22b-distilled-nvfp4")
	require.True(t, ok)
	require.NotNil(t, nv.Requires)
	assert.Equal(t, []string{platform.ArchBlackwell}, nv.Requires.GPUArch)
	assert.Nil(t, nv.Unsupported)

	fp8 := 0
	for _, m := range c.Models {
		hasFP8 := false
		for _, f := range m.Files {
			hasFP8 = hasFP8 || strings.Contains(strings.ToLower(f.Name), "fp8")
		}
		switch {
		case hasFP8:
			fp8++
			require.NotNil(t, m.Unsupported, "%s loads an fp8 file: it does not run on a Mac (MPS)", m.ID)
			assert.Equal(t, []string{platform.OSDarwin}, m.Unsupported.OS, m.ID)
			assert.Contains(t, m.Unsupported.Source, "#10292", m.ID)
		case m.ID == "ltx-2.5-22b-distilled-nvfp4":
		default:
			assert.Nil(t, m.Unsupported, "%s has no fp8 file: nothing says it fails on a Mac", m.ID)
			assert.Nil(t, m.Requires, m.ID)
		}
	}
	assert.GreaterOrEqual(t, fp8, 18, "the fp8 models of the catalog")
	for _, id := range []string{"sdxl-base", "ltx-2.5-22b-distilled", "noctq-v4-qwen-image21"} {
		m, _ := c.Model(id)
		assert.Nil(t, m.Unsupported, id)
		assert.Nil(t, m.Requires, id)
	}
}

// catalog.Validate runs the check of the columns for every model: a column with an unknown value is a problem of the catalog.
func TestCatalogValidateChecksTheColumns(t *testing.T) {
	data, err := json.Marshal(catalog.Default())
	require.NoError(t, err)
	var c catalog.Catalog
	require.NoError(t, json.Unmarshal(data, &c))
	assert.Empty(t, catalog.Validate(&c, localcfg.TemplateByID))

	c.Models[0].Unsupported = &platform.Unsupported{OS: []string{"beos"}, Reason: "r", Source: "s 2026-11-03"}
	errs := catalog.Validate(&c, localcfg.TemplateByID)
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], `model `+c.Models[0].ID+`: unsupported.os: "beos" is not a known value`)
}
