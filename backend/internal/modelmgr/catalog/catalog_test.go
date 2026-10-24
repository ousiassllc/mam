package catalog_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// The embedded catalog is valid: every file is pinned (https URL at a commit, SHA-256, size), every license
// is filled, every workflow is an existing, structurally valid template whose loaders name the model's files.
func TestEmbeddedCatalogIsValid(t *testing.T) {
	c := catalog.Default()
	assert.Empty(t, catalog.Validate(c, localcfg.TemplateByID))

	assert.Equal(t, "v0.42.1", c.Engine.Version)
	require.Len(t, c.Models, 8)
	var ids []string
	for _, m := range c.Models {
		ids = append(ids, m.ID)
	}
	assert.Equal(t, []string{
		"flux1-schnell", "qwen-image", "sdxl-base", "ltx-video-2b", "wan22-ti2v-5b", // standard: images, then videos
		"flux1-dev", "sd35-medium", "hunyuanvideo15-480p",
	}, ids)
}

func TestTheCatalogHoldsTheDecidedModelsAndLicenses(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string]struct {
		license string
		consent bool
		commerc bool
		files   int
	}{
		"sdxl-base":           {"creativeml-openrail++-m", false, true, 1},
		"flux1-schnell":       {"apache-2.0", false, true, 1},
		"qwen-image":          {"apache-2.0", false, true, 3},
		"wan22-ti2v-5b":       {"apache-2.0", false, true, 3},
		"ltx-video-2b":        {"ltx-video-open-rail-m", false, true, 2},
		"flux1-dev":           {"flux-1-dev-non-commercial", true, false, 1},
		"sd35-medium":         {"stabilityai-ai-community", true, true, 1},
		"hunyuanvideo15-480p": {"tencent-hunyuan-community", true, true, 4},
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		assert.Equal(t, want.license, m.License.ID, id)
		assert.Equal(t, want.consent, m.ConsentRequired, id)
		assert.Equal(t, want.commerc, m.License.CommercialUse, id)
		assert.Len(t, m.Files, want.files, id)
		assert.False(t, m.RequiresHFToken, id)
		assert.Positive(t, m.TotalSize(), id)
	}
	hunyuan, _ := c.Model("hunyuanvideo15-480p")
	assert.Equal(t, []string{"EU", "GB", "KR"}, hunyuan.License.ExcludedRegions)
	sd35, _ := c.Model("sd35-medium")
	assert.Contains(t, sd35.License.CommercialCondition, "100 万米ドル")

	// The text encoder both Qwen-Image and HunyuanVideo load is one file on disk.
	qwen, _ := c.Model("qwen-image")
	assert.Equal(t, "models/text_encoders/qwen_2.5_vl_7b_fp8_scaled.safetensors", qwen.Files[1].Path())
	assert.Equal(t, qwen.Files[1].SHA256, hunyuan.Files[1].SHA256)
}

func TestEngineIsPinnedPerGPUBuild(t *testing.T) {
	e := catalog.Default().Engine
	for _, v := range e.VariantList() {
		a := e.Variants[v]
		assert.Contains(t, a.URL, "/releases/download/v0.42.1/ComfyUI_windows_portable_"+string(v)+".7z")
		assert.Len(t, a.SHA256, 64)
		assert.Greater(t, a.ExtractedSize, a.Size)
	}
	assert.Equal(t, int64(2018444114), e.Variants[catalog.VariantNVIDIA].Size)
	assert.Equal(t, "https://github.com/Comfy-Org/ComfyUI/tree/v0.42.1", e.SourceURL)
}

// mutate returns the validation problems of the embedded catalog after edit changed a copy of it.
func mutate(t *testing.T, edit func(c *catalog.Catalog)) []error {
	t.Helper()
	data, err := json.Marshal(catalog.Default())
	require.NoError(t, err)
	var c catalog.Catalog
	require.NoError(t, json.Unmarshal(data, &c))
	edit(&c)
	return catalog.Validate(&c, localcfg.TemplateByID)
}

func TestValidateRefusesWhatIsNotPinned(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(c *catalog.Catalog)
		want string
	}{
		"branch instead of a commit": {func(c *catalog.Catalog) {
			c.Models[0].Files[0].URL = strings.Replace(c.Models[0].Files[0].URL, c.Models[0].Files[0].URL[strings.Index(c.Models[0].Files[0].URL, "resolve/")+8:][:40], "main", 1)
		}, "40-hex commit"},
		"plain http": {func(c *catalog.Catalog) {
			c.Models[0].Files[0].URL = strings.Replace(c.Models[0].Files[0].URL, "https", "http", 1)
		}, "40-hex commit"},
		"another host": {func(c *catalog.Catalog) {
			c.Models[0].Files[0].URL = strings.Replace(c.Models[0].Files[0].URL, "huggingface.co", "example.com", 1)
		}, "40-hex commit"},
		"no sha256":        {func(c *catalog.Catalog) { c.Models[0].Files[0].SHA256 = "" }, "sha256"},
		"upper-case sha":   {func(c *catalog.Catalog) { c.Models[0].Files[0].SHA256 = strings.ToUpper(c.Models[0].Files[0].SHA256) }, "sha256"},
		"no size":          {func(c *catalog.Catalog) { c.Models[0].Files[0].Size = 0 }, "size is required"},
		"path in the name": {func(c *catalog.Catalog) { c.Models[0].Files[0].Name = "../x.safetensors" }, "file name"},
		"unknown folder":   {func(c *catalog.Catalog) { c.Models[0].Files[0].Folder = "custom_nodes" }, "not a model folder"},
		"engine sha":       {func(c *catalog.Catalog) { a := c.Engine.Variants["amd"]; a.SHA256 = "x"; c.Engine.Variants["amd"] = a }, "engine variant amd"},
		"engine branch":    {func(c *catalog.Catalog) { c.Engine.Version = "master" }, "release tag"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}

func TestValidateChecksLicensesConsentAndWorkflows(t *testing.T) {
	find := func(c *catalog.Catalog, id string) *catalog.Model {
		for i := range c.Models {
			if c.Models[i].ID == id {
				return &c.Models[i]
			}
		}
		t.Fatalf("no model %s", id)
		return nil
	}
	for name, tc := range map[string]struct {
		edit func(c *catalog.Catalog)
		want string
	}{
		"missing license summary":                  {func(c *catalog.Catalog) { find(c, "sdxl-base").License.Summary = "" }, "license needs"},
		"a non-commercial license without consent": {func(c *catalog.Catalog) { find(c, "flux1-dev").ConsentRequired = false }, "requires consent"},
		"consent on a plain model":                 {func(c *catalog.Catalog) { find(c, "sdxl-base").ConsentRequired = true }, "requires consent"},
		"a login-only model":                       {func(c *catalog.Catalog) { find(c, "sdxl-base").RequiresHFToken = true }, "Hugging Face login"},
		"no vram source":                           {func(c *catalog.Catalog) { find(c, "sdxl-base").VRAM.Source = "" }, "source of the guideline"},
		"unknown template":                         {func(c *catalog.Catalog) { find(c, "sdxl-base").Workflows[0].Template = "nope" }, "does not exist"},
		"template of the wrong media":              {func(c *catalog.Catalog) { find(c, "sdxl-base").Workflows[0].Template = "txt2video-wan22-5b" }, "makes video"},
		"duplicate workflow name":                  {func(c *catalog.Catalog) { find(c, "flux1-dev").Workflows[0].Name = "sdxl-base" }, "used twice"},
		"a file the workflow does not load": {func(c *catalog.Catalog) {
			m := find(c, "sdxl-base")
			extra := m.Files[0]
			extra.Name = "extra.safetensors"
			m.Files = append(m.Files, extra)
		}, "is not loaded by the workflow"},
		"a loader of a file the model lacks": {func(c *catalog.Catalog) { find(c, "sdxl-base").Files[0].Name = "other.safetensors" }, "not a file of the model"},
		"a shared file with another hash": {func(c *catalog.Catalog) {
			find(c, "hunyuanvideo15-480p").Files[1].SHA256 = strings.Repeat("a", 64)
		}, "the same destination"},
		"duplicate model id": {func(c *catalog.Catalog) { find(c, "flux1-dev").ID = "sdxl-base" }, "duplicate id"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}

func TestLoadRefusesUnknownFields(t *testing.T) {
	fsys := fstest.MapFS{
		"data/engine.json":   {Data: []byte(`{"version":"v1.0.0","bogus":1}`)},
		"data/models/x.json": {Data: []byte(`{}`)},
	}
	_, err := catalog.Load(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}
