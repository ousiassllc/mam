package lightmodelstest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Wan 2.2 TI2V 5B fp8 (Issue #967) is the fp16 model with a smaller diffusion model: the same text encoder and VAE (one copy on disk), Kijai's fp8
// (e4m3fn, scaled) conversion pinned to its commit with the LFS SHA-256 and size Hugging Face returns (X-Linked-Etag; the SHA-256 was also computed on the
// downloaded file on 2026-11-02, and ComfyUI v0.42.1 loaded it as a WAN22 model on the CPU). Time and VRAM are not measured: the VRAM figure is an estimate.
func TestWan5BFP8IsTheSameModelWithAnFP8DiffusionModel(t *testing.T) {
	c := catalog.Default()
	fp16, _ := c.Model("wan22-ti2v-5b")
	m, ok := c.Model("wan22-ti2v-5b-fp8")
	require.True(t, ok)
	assert.Equal(t, catalog.TierStandard, m.Tier)
	assert.False(t, m.ConsentRequired)
	assert.False(t, m.RequiresHFToken)
	assert.Equal(t, modellicense.Yes, m.License.Commercial)
	assert.Equal(t, "apache-2.0", m.License.ID)
	assert.True(t, m.VRAM.Estimate, "not measured on a 16 GB card")
	assert.Equal(t, fp16.VRAM.MinGB, m.VRAM.MinGB, "no lower figure is claimed without a measurement")
	assert.Contains(t, m.VRAM.Source, "未確認")

	require.Len(t, m.Files, 3)
	unet := m.Files[0]
	assert.Equal(t, "diffusion_models", unet.Folder)
	assert.Equal(t, "Wan2_2-TI2V-5B_fp8_e4m3fn_scaled_KJ.safetensors", unet.Name)
	assert.Equal(t, "https://huggingface.co/Kijai/WanVideo_comfy_fp8_scaled/resolve/033a4e487f60220b3d6e469599a6aebc46e13cee/TI2V/Wan2_2-TI2V-5B_fp8_e4m3fn_scaled_KJ.safetensors", unet.URL)
	assert.Equal(t, "a83f54a2450d5471e5721e59ab556afa2d8793e30280713e3796b254c5286b48", unet.SHA256)
	assert.Equal(t, int64(5277255650), unet.Size)
	assert.Equal(t, fp16.Files[1:], m.Files[1:], "the text encoder and the VAE are the very files of the fp16 model")
	assert.Less(t, m.TotalSize(), fp16.TotalSize())
	assert.Less(t, unet.Size*18/10, fp16.Files[0].Size, "about half of the fp16 diffusion model")
	assert.Contains(t, m.Description, "Windows のみ", "fp8 does not run on MPS")
	assert.Equal(t, modellicense.Yes, m.License.Components[0].Commercial)
	assert.Contains(t, m.License.Components[0].URL, "Kijai/WanVideo_comfy_fp8_scaled")
}

// The two workflows are the fp16 ones with the diffusion model replaced, and still the T2V one and the first-frame one.
func TestWan5BFP8TemplatesDifferOnlyInTheDiffusionModel(t *testing.T) {
	m, _ := catalog.Default().Model("wan22-ti2v-5b-fp8")
	byName := map[string]string{}
	for _, wf := range m.Workflows {
		byName[wf.Name] = wf.Template
	}
	assert.Equal(t, map[string]string{"wan22-ti2v-5b-fp8": "txt2video-wan22-5b-fp8", "wan22-ti2v-5b-fp8-i2v": "img2video-wan22-5b-fp8"}, byName)
	for _, kind := range []string{"txt2video", "img2video"} {
		base, _ := localcfg.TemplateByID(kind + "-wan22-5b")
		fp8, ok := localcfg.TemplateByID(kind + "-wan22-5b-fp8")
		require.True(t, ok, kind)
		assert.Equal(t,
			strings.ReplaceAll(string(base.Model.Workflow.Graph), "wan2.2_ti2v_5B_fp16.safetensors", "Wan2_2-TI2V-5B_fp8_e4m3fn_scaled_KJ.safetensors"),
			string(fp8.Model.Workflow.Graph), kind)
		assert.Equal(t, base.Model.Workflow.Assignments, fp8.Model.Workflow.Assignments, kind)
		assert.Equal(t, base.Model.Workflow.FPS, fp8.Model.Workflow.FPS, kind)
	}
}

// FLUX.2 klein in bf16 for macOS (Issue #967) is not offered: the model manager installs the bundled ComfyUI only on Windows, so a macOS-only
// entry could not be installed at all, and a Windows user already has the fp8 one. The catalog therefore has no bf16 klein file.
func TestFlux2KleinHasNoBF16EntryBecauseTheManagerDoesNotInstallOnMacOS(t *testing.T) {
	c := catalog.Default()
	for _, v := range c.Engine.VariantList() {
		assert.Contains(t, c.Engine.Variants[v].URL, "windows_portable", "the bundled engine is a Windows build: "+string(v))
	}
	for _, m := range c.Models {
		for _, f := range m.Files {
			assert.NotEqual(t, "flux-2-klein-4b.safetensors", f.Name, m.ID+": a bf16 klein for macOS would never be installed by the manager")
		}
	}
}
