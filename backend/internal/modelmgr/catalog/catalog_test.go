package catalog_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"

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
	require.Len(t, c.Models, 71)
	var ids []string
	for _, m := range c.Models {
		ids = append(ids, m.ID)
	}
	assert.Equal(t, []string{
		"flux1-schnell", "flux2-klein-4b", "qwen-image", "qwen-image-edit-2511", "sdxl-base", "sdxl-controlnet-canny", "sdxl-controlnet-depth", "sdxl-controlnet-normal", "sdxl-ipadapter-plus", "upscale-2pass-sdxl-span", "upscale-nomos-uni-span", "ltx-video-2b", "wan21-flf2v-14b-720p", "wan21-t2v-1-3b", "wan21-vace-1-3b", "wan22-i2v-14b", "wan22-i2v-14b-lightx2v", "wan22-t2v-14b", "wan22-ti2v-5b", "wan22-ti2v-5b-fp8", "rife-v4.25", "rife-v4.26", "seedvr2-3b-fp8", // standard: images, then videos, then the video enhancement models (video_interpolate, video_upscale)
		"big-lust-v16", "bigasp-v2", "chroma1-hd", "cyberrealistic-pony-v18", "cyberrealistic-xl-v10", "epicrealism-xl-lastfame", "flux1-dev", "flux1-dev-nsfw-master", "juggernaut-xl-ragnarok", "lustify-sdxl-zenith-v9", "lustify-v10-krea2-turbo-fp8", "noctq-v4-qwen-image21", // advanced: images, then videos
		"noobai-xl-v11", "pony-diffusion-v6-xl", "qwen-image-21-uncensored-gguf-q4-k-m", "qwen-image-21-uncensored-gguf-q8-0", "qwen-image-21-uncensored-int8", "qwen-image-21-vnccs-pose", "qwen-image-edit-rapid-aio-nsfw-v23", "realism-sdxl-stable-yogi-v8", "realvisxl-v50", "realvisxl-v50-lightning", "sd35-medium", "thisisreal-sdxl-v3", "wai-illustrious-sdxl-v17", "upscale-nomos-webphoto-esrgan",
		"hunyuanvideo-t2v-nsfw-lora", "hunyuanvideo15-480p", "hunyuanvideo15-720p-i2v", "hunyuanvideo15-720p-t2v", "ltx-2.3-22b-distilled", "ltx-2.5-22b-distilled", "ltx-2.5-22b-distilled-dfr", "ltx-2.5-22b-distilled-nvfp4", "ltx-2.5-22b-distilled-uncensored", "nsfw-anime-wan22-5b", "nsfw-wan21-14b-t2v", "nsfw-wan21-1b3-t2v", "wan22-i2v-14b-lightx2v-uncensored", "wan22-i2v-14b-nsfw-lora", "wan22-i2v-r1-v2-uncensored", "wan22-rapid-aio-v10-nsfw-i2v", "wan22-rapid-mega-nsfw-t2v", "wan22-remix-nsfw-i2v", "wan22-remix-nsfw-t2v", "wan22-t2v-14b-mystic-xxx-lora", "wan22-t2v-14b-palingenesis", "wan22-ti2v-5b-cowgirl-lora",
	}, ids)
}

func TestTheCatalogHoldsTheDecidedModelsAndLicenses(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string]struct {
		license string
		consent bool
		commerc modellicense.Commercial
		files   int
	}{
		"sdxl-base":                         {"creativeml-openrail++-m", false, modellicense.Yes, 1},
		"flux1-schnell":                     {"apache-2.0", false, modellicense.Yes, 1},
		"flux2-klein-4b":                    {"apache-2.0", false, modellicense.Yes, 3},
		"qwen-image":                        {"apache-2.0", false, modellicense.Yes, 3},
		"qwen-image-edit-2511":              {"apache-2.0", false, modellicense.Yes, 5},
		"wan22-ti2v-5b":                     {"apache-2.0", false, modellicense.Yes, 3},
		"wan22-i2v-14b":                     {"apache-2.0", false, modellicense.Yes, 4},
		"wan22-i2v-14b-lightx2v":            {"apache-2.0", false, modellicense.Yes, 6},
		"ltx-video-2b":                      {"ltx-video-open-rail-m", false, modellicense.Yes, 2},
		"flux1-dev":                         {"flux-1-dev-non-commercial", true, modellicense.No, 1},
		"sdxl-controlnet-depth":             {"sdxl-controlnet-depth", false, modellicense.Yes, 2},
		"sdxl-controlnet-normal":            {"sdxl-controlnet-normal", false, modellicense.Yes, 2},
		"sd35-medium":                       {"stabilityai-ai-community", true, modellicense.Conditional, 1},
		"hunyuanvideo15-480p":               {"tencent-hunyuan-community", true, modellicense.Conditional, 4},
		"noctq-v4-qwen-image21":             {"qwen-research", true, modellicense.No, 3},
		"chroma1-hd":                        {"apache-2.0", true, modellicense.Yes, 3},
		"flux1-dev-nsfw-master":             {"flux-1-dev-non-commercial", true, modellicense.No, 2},
		"wan22-remix-nsfw-i2v":              {"unspecified", true, modellicense.Unknown, 4},
		"wan22-remix-nsfw-t2v":              {"unspecified", true, modellicense.Unknown, 4},
		"wan22-i2v-14b-lightx2v-uncensored": {"apache-2.0-declared", true, modellicense.Unknown, 6},
		"wan22-i2v-14b-nsfw-lora":           {"unspecified", true, modellicense.Unknown, 6},
		"wan22-ti2v-5b-fp8":                 {"apache-2.0", false, modellicense.Yes, 3},
		"ltx-2.5-22b-distilled":             {"ltx-2x-community", true, modellicense.Conditional, 6},
		"ltx-2.5-22b-distilled-nvfp4":       {"ltx-2x-community", true, modellicense.Conditional, 6},
		"ltx-2.5-22b-distilled-dfr":         {"ltx-2x-community", true, modellicense.Conditional, 8},
		"ltx-2.5-22b-distilled-uncensored":  {"ltx-2x-community-uncensored-te", true, modellicense.Unknown, 5},
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		assert.Equal(t, want.license, m.License.ID, id)
		assert.Equal(t, want.consent, m.ConsentRequired, id)
		assert.Equal(t, want.commerc, m.License.Commercial, id)
		assert.Len(t, m.Files, want.files, id)
		assert.Equal(t, strings.HasPrefix(id, "ltx-2.5-"), m.RequiresHFToken, id+": only the gated LTX-2.5 (with and without the uncensored text encoder) is behind the Hugging Face login")
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
		"a login-only model mirrored by a third party": {func(c *catalog.Catalog) {
			m := find(c, "sdxl-base")
			m.RequiresHFToken = true
			m.Files[0].SourceNote = "第三者のミラー。原本は Civitai で、SHA-256 が一致することを 2026-10-25 に確かめた。"
		}, "not mirrored by a third party"},
		"no vram source":              {func(c *catalog.Catalog) { find(c, "sdxl-base").VRAM.Source = "" }, "source of the guideline"},
		"unknown template":            {func(c *catalog.Catalog) { find(c, "sdxl-base").Workflows[0].Template = "nope" }, "does not exist"},
		"template of the wrong media": {func(c *catalog.Catalog) { find(c, "sdxl-base").Workflows[0].Template = "txt2video-wan22-5b" }, "makes video"},
		"duplicate workflow name":     {func(c *catalog.Catalog) { find(c, "flux1-dev").Workflows[0].Name = "sdxl-base" }, "used twice"},
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
		// #928: the answer for the whole model is the strictest of its parts, and every license says when it was read.
		"no check date":                     {func(c *catalog.Catalog) { find(c, "sdxl-base").License.CheckedAt = "" }, "checked_at"},
		"no commercial answer":              {func(c *catalog.Catalog) { find(c, "sdxl-base").License.Commercial = "" }, "commercial"},
		"a conditional without a condition": {func(c *catalog.Catalog) { find(c, "sd35-medium").License.CommercialCondition = "" }, "condition"},
		"an answer looser than a part": {func(c *catalog.Catalog) {
			find(c, "flux1-dev-nsfw-master").License.Commercial = modellicense.Yes
		}, "strictest of the components is no"},
		"a part that is not described": {func(c *catalog.Catalog) {
			m := find(c, "flux1-dev-nsfw-master")
			m.License.Components[0].URL = "http://example.com"
		}, "license component"},
		"a license without a text that is commercial": {func(c *catalog.Catalog) {
			find(c, "wan22-remix-nsfw-i2v").License.Commercial = modellicense.Conditional
			find(c, "wan22-remix-nsfw-i2v").License.Components = nil
		}, "not commercial use"},
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

// #928: every model of the catalog has its license: the name, the answer, the original, the day it was read. A model that a later issue
// adds without them is caught here (and by Validate), so the badges and the filter never meet a model without an answer.
func TestEveryModelHasItsLicenseWithTheSourceAndTheDateItWasRead(t *testing.T) {
	for _, m := range catalog.Default().Models {
		l := m.License
		assert.NoError(t, l.Info().Check(), m.ID)
		if l.Commercial != modellicense.Yes {
			assert.True(t, m.Tier == catalog.TierAdvanced && m.ConsentRequired, m.ID+": what is not 商用OK is shown before the install and needs consent")
		}
	}
}

// A model made of parts with different licenses shows the strictest one (a LoRA of unknown license on an Apache base is unknown, a LoRA
// on a non-commercial base is non-commercial), and the parts are listed so that the reader can see why.
func TestTheStricterOfTheBaseAndTheDerivativeIsShown(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string]modellicense.Commercial{
		"flux1-dev-nsfw-master":             modellicense.No,      // FLUX.1 [dev] is non-commercial; the LoRA's license is not known
		"wan22-i2v-14b-nsfw-lora":           modellicense.Unknown, // the base is Apache-2.0; the LoRA has no license text
		"wan22-remix-nsfw-i2v":              modellicense.Unknown,
		"wan22-remix-nsfw-t2v":              modellicense.Unknown,
		"wan22-i2v-14b-lightx2v-uncensored": modellicense.Unknown, // Apache-2.0 on the card only: no text, no word on the training data
		"wan22-i2v-14b-lightx2v":            modellicense.Yes,     // the base and the LoRA are both Apache-2.0
		"noctq-v4-qwen-image21":             modellicense.No,      // the weights, the text encoder and the VAE are all Qwen RESEARCH
		"ltx-2.5-22b-distilled":             modellicense.Conditional,
		"ltx-2.5-22b-distilled-uncensored":  modellicense.Unknown, // LTX-2.x is conditional; the converted text encoder has no license text
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		assert.Equal(t, want, m.License.Commercial, id)
		assert.GreaterOrEqual(t, len(m.License.Components), 2, id+": the parts are listed")
	}
}

// Qwen-Image-2.1 and what is made from it (Noct Q) are under the Qwen RESEARCH LICENSE AGREEMENT, whose section 2 grants the right for
// non-commercial purposes only (read in the LICENSE of the repository on 2026-11-02): the catalog says 商用NG, with the source and the date.
func TestNoctQQwenImage21IsNotForCommercialUse(t *testing.T) {
	m, ok := catalog.Default().Model("noctq-v4-qwen-image21")
	require.True(t, ok)

	assert.Equal(t, modellicense.No, m.License.Commercial)
	assert.Contains(t, m.License.URL, "huggingface.co/Noctaluna/Noct-Q-Uncensored-Qwen-Image-2.1")
	assert.Equal(t, "2026-11-02", m.License.CheckedAt)
	assert.NotEmpty(t, m.License.Notice)
	for _, part := range m.License.Components {
		assert.Equal(t, modellicense.No, part.Commercial, part.Part)
	}
}
