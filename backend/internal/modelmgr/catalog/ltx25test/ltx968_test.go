package ltx25test_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

const (
	ltx25Repo           = "https://huggingface.co/Lightricks/LTX-2.5/resolve/49b093a39c19169a8a81721dcc541e07e945232f/"
	icLoRARepo          = "https://huggingface.co/Lightricks/LTX-2.5-22b-IC-LoRA-Pixel-Spatial-Upscaler/resolve/59f26f1e0c11af42f1affa924b4ef231e9e98b2a/"
	enhancerFile        = "gemma4_e2b_it_int8_convrot.safetensors"
	enhancerSHA256      = "efeca0fcad2f863e5ed0a75e3af952b72bc963604c1dda6d20aee87a32b17566"
	enhancerSize        = int64(5199997904)
	enhancerPinnedAtURL = "https://huggingface.co/Comfy-Org/gemma-4/resolve/63d0f7c476756b88910170c1df75e2384ea1af31/text_encoders/" + enhancerFile
)

func fileOf(t *testing.T, m catalog.Model, name string) catalog.File {
	t.Helper()
	for _, f := range m.Files {
		if f.Name == name {
			return f
		}
	}
	require.Failf(t, "no such file", "%s in %s", name, m.ID)
	return catalog.File{}
}

func model(t *testing.T, id string) catalog.Model {
	t.Helper()
	m, ok := catalog.Default().Model(id)
	require.True(t, ok, id)
	return m
}

// The prompt enhancer (Issue #968) is Comfy-Org's copy of Google's Gemma 4 E2B (Apache-2.0, ungated), pinned to its own commit; the standard model carries it, and so
// do the NVFP4 and DFR models (Issue #979: separate -enhanced workflows, not a switch). The uncensored one does not (it would send the prompt through a censored model).
func TestPromptEnhancerIsComfyOrgsUngatedCopy(t *testing.T) {
	std := model(t, "ltx-2.5-22b-distilled")
	f := fileOf(t, std, enhancerFile)
	assert.Equal(t, "text_encoders", f.Folder)
	assert.Equal(t, enhancerPinnedAtURL, f.URL)
	assert.Equal(t, enhancerSHA256, f.SHA256)
	assert.Equal(t, enhancerSize, f.Size)
	assert.Empty(t, f.SourceNote)
	assert.Contains(t, std.License.Components[len(std.License.Components)-1].Part, "プロンプト拡張")
	assert.Equal(t, modellicense.Conditional, std.License.Commercial, "the enhancer is Apache-2.0: the strictest answer is still the revenue condition of LTX-2.x")

	for _, f := range model(t, "ltx-2.5-22b-distilled-uncensored").Files {
		assert.NotEqual(t, enhancerFile, f.Name, "uncensored")
	}
	enhancedOf := func(m catalog.Model) []string {
		var enhanced []string
		for _, w := range m.Workflows {
			if strings.HasSuffix(w.Name, "-enhanced") {
				enhanced = append(enhanced, w.Template)
			}
		}
		return enhanced
	}
	assert.Equal(t, []string{"txt2video-ltx-2-5-enhanced", "img2video-ltx-2-5-enhanced", "flf2v-ltx-2-5-enhanced"}, enhancedOf(std))
	assert.Empty(t, enhancedOf(model(t, "ltx-2.5-22b-distilled-uncensored")))

	// NVFP4 and DFR carry the same file (one copy on disk) and the license part of it, and name their own -enhanced workflows after the plain ones.
	nv, dfr := model(t, "ltx-2.5-22b-distilled-nvfp4"), model(t, "ltx-2.5-22b-distilled-dfr")
	for _, m := range []catalog.Model{nv, dfr} {
		assert.Contains(t, m.Files, f, m.ID+": the same file as the standard model")
		assert.Equal(t, modellicense.Conditional, m.License.Commercial, m.ID)
		assert.Contains(t, m.License.Components[len(m.License.Components)-1].Part, "プロンプト拡張", m.ID)
	}
	assert.Equal(t, []string{"txt2video-ltx-2-5-nvfp4-enhanced", "img2video-ltx-2-5-nvfp4-enhanced", "flf2v-ltx-2-5-nvfp4-enhanced"}, enhancedOf(nv))
	assert.Equal(t, []string{"img2video-ltx-2-5-dfr-enhanced"}, enhancedOf(dfr))
}

// NVFP4 (Issue #968): the distilled NVFP4 diffusion model of the gated repository, pinned like the INT8 one; the other four files are the
// standard model's. NVFP4 needs a Blackwell GPU (ComfyUI v0.42.1 skips nvfp4 below compute capability 10), which the card and the limits say, and the
// memory figure is the INT8 one, marked as an estimate (not run: the development GPU is not Blackwell).
func TestNVFP4ModelIsTheDistilledModelWithANVFP4DiffusionModel(t *testing.T) {
	std, m := model(t, "ltx-2.5-22b-distilled"), model(t, "ltx-2.5-22b-distilled-nvfp4")
	assert.True(t, m.RequiresHFToken)
	assert.True(t, m.ConsentRequired)
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.Equal(t, catalog.RatingGeneral, m.ContentRating)
	assert.Equal(t, std.License.ID, m.License.ID)
	assert.Equal(t, std.License.Version, m.License.Version, "accepting the license of one is accepting it for the other")
	assert.Equal(t, modellicense.Conditional, m.License.Commercial)

	unet := fileOf(t, m, "ltx-2.5-22b-distilled-transformer-nvfp4.safetensors")
	assert.Equal(t, ltx25Repo+"diffusion_models/ltx-2.5-22b-distilled-transformer-nvfp4.safetensors", unet.URL)
	assert.Equal(t, "4b94231e734c1950f8f6826cb8bd8715d94be5b3e04f8256ee060c5bc3886c30", unet.SHA256)
	assert.Equal(t, int64(18721548408), unet.Size)
	assert.Equal(t, "diffusion_models", unet.Folder)
	shared := 0
	for _, f := range std.Files {
		if f.Folder != "diffusion_models" {
			assert.Contains(t, m.Files, f, "the same file as the standard model: one copy on disk")
			shared++
		}
	}
	assert.Equal(t, 5, shared, "the four files and the prompt enhancer")
	assert.Len(t, m.Files, 6, "the five of the plain workflows and the prompt enhancer (Issue #979)")

	assert.Contains(t, m.Description, "Blackwell")
	assert.Contains(t, m.Description, "未確認")
	assert.Contains(t, m.VRAM.Limits, "Blackwell")
	assert.Contains(t, m.VRAM.Source, "supports_nvfp4_compute")
	assert.True(t, m.VRAM.Estimate)
	assert.Equal(t, std.VRAM.MinGB, m.VRAM.MinGB)
	assert.Equal(t, std.VRAM.RecommendedGB, m.VRAM.RecommendedGB)
	assert.Less(t, unet.Size, fileOf(t, std, "ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors").Size)
	assert.Equal(t, []string{"txt2video-ltx-2-5-nvfp4", "img2video-ltx-2-5-nvfp4", "flf2v-ltx-2-5-nvfp4",
		"txt2video-ltx-2-5-nvfp4-enhanced", "img2video-ltx-2-5-nvfp4-enhanced", "flf2v-ltx-2-5-nvfp4-enhanced"}, templatesOf(m))
}

// DFR (Issue #968) needs the temporal upscaler (same gated repository as the rest) and the detailing IC-LoRA, which lives in a second gated repository of
// Lightricks (its own acceptance); every file is pinned to the commit and the LFS SHA-256 and size Hugging Face returns for it.
func TestDFRModelReadsTwoGatedRepositories(t *testing.T) {
	std, m := model(t, "ltx-2.5-22b-distilled"), model(t, "ltx-2.5-22b-distilled-dfr")
	assert.True(t, m.RequiresHFToken)
	assert.True(t, m.ConsentRequired)
	assert.Equal(t, modellicense.Conditional, m.License.Commercial)
	assert.Equal(t, std.VRAM.MinGB, m.VRAM.MinGB)
	assert.True(t, m.VRAM.Estimate, "three stages: not measured")

	temporal := fileOf(t, m, "ltx-2.5-latent-temporal-upscaler-x2-bf16-1.0.safetensors")
	assert.Equal(t, "latent_upscale_models", temporal.Folder)
	assert.Equal(t, ltx25Repo+"latent_upscale_models/ltx-2.5-latent-temporal-upscaler-x2-bf16-1.0.safetensors", temporal.URL)
	assert.Equal(t, "2bc3300f2b3c3c1834d72164fbf13a3b9fd73e5a741e8a2c3f4035f89a75c3fe", temporal.SHA256)
	assert.Equal(t, int64(261944000), temporal.Size)

	lora := fileOf(t, m, "ltx-2.5-22b-ic-lora-pixel-spatial-upscaler-x2-1.0.safetensors")
	assert.Equal(t, "loras", lora.Folder)
	assert.Equal(t, icLoRARepo+"ltx-2.5-22b-ic-lora-pixel-spatial-upscaler-x2-1.0.safetensors", lora.URL)
	assert.Equal(t, "984851b769ea2bcb4c9e0a239a7676239e42c6a6001ddc69943b41ff0b283c1d", lora.SHA256)
	assert.Equal(t, int64(327322640), lora.Size)
	assert.Empty(t, lora.SourceNote, "the repository of Lightricks itself, not a mirror")
	assert.Contains(t, m.Description, "別のリポジトリ")
	assert.Contains(t, m.License.Notice, "Lightricks/LTX-2.5-22b-IC-LoRA-Pixel-Spatial-Upscaler")

	for _, f := range std.Files {
		assert.Contains(t, m.Files, f, "shared with the standard model")
	}
	assert.Len(t, m.Files, 8, "the seven of the plain workflow and the prompt enhancer (Issue #979)")
	assert.Equal(t, []string{"img2video-ltx-2-5-dfr", "img2video-ltx-2-5-dfr-enhanced"}, templatesOf(m))
}

// The first and last frames (FLF2V) are offered with the standard, the uncensored and the NVFP4 models; the single stage makes the upscaler unused by that
// workflow, which the catalog check allows as long as another workflow of the model loads it.
func TestFLF2VIsOfferedWithEachFamilyMember(t *testing.T) {
	for id, want := range map[string]string{
		"ltx-2.5-22b-distilled":            "flf2v-ltx-2-5",
		"ltx-2.5-22b-distilled-uncensored": "flf2v-ltx-2-5-uncensored",
		"ltx-2.5-22b-distilled-nvfp4":      "flf2v-ltx-2-5-nvfp4",
	} {
		assert.Contains(t, templatesOf(model(t, id)), want, id)
		tpl, ok := localcfg.TemplateByID(want)
		require.True(t, ok, id)
		caps, _ := tpl.Model.VideoCaps()
		assert.True(t, caps.LastFrame, id)
	}
	assert.NotContains(t, templatesOf(model(t, "ltx-2.5-22b-distilled-dfr")), "flf2v-ltx-2-5")
}

// A file that no workflow of a model loads is still refused: the single-stage FLF2V alone would not use the upscaler.
func TestAFileNoWorkflowOfTheModelLoadsIsRefused(t *testing.T) {
	errs := mutate(t, func(c *catalog.Catalog) {
		for i := range c.Models {
			if c.Models[i].ID == "ltx-2.5-22b-distilled" {
				c.Models[i].Workflows = []catalog.Workflow{{Name: "ltx-2-5-flf2v", Template: "flf2v-ltx-2-5"}}
			}
		}
	})
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "is not loaded by the workflow of the model")
}

func templatesOf(m catalog.Model) []string {
	var out []string
	for _, w := range m.Workflows {
		out = append(out, w.Template)
	}
	return out
}
