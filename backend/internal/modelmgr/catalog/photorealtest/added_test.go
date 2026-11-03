package photorealtest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// The uncensored models Issue #984 left out and that were added later (2026-11-03): each file is pinned at a commit, with the SHA-256 and the
// size Hugging Face reports (the LFS oid), and a model of Civitai names the original it was compared to.
var addedPins = []struct {
	id, name, url, sha string
	size               int64
	civitai            string // the part of the source note that names the original; "" when the file comes from the author
}{
	{"realvisxl-v50-lightning", "RealVisXL_V5.0_Lightning_fp16.safetensors", "https://huggingface.co/SG161222/RealVisXL_V5.0_Lightning/resolve/f4454158cedaab9f0688c199561d6c92525f3a85/RealVisXL_V5.0_Lightning_fp16.safetensors", "fabcadd9330dcc4f9702063428d40b9d4d07168d8acefc819b8d1d9db466b3ec", 6938065512, "model 139562、version 798204"},
	{"lustify-v10-krea2-turbo-fp8", "lustifyNSFWCheckpoint_v10Krea2.safetensors", "https://huggingface.co/ogkgm/lustifybeyondrealism/resolve/e0994265ed8b1897654b61c9245fbf2727f53efa/lustifyNSFWCheckpoint_v10Krea2.safetensors", "94d92700fc45200ef053895ec5655d4f64a69b924c1efaa457521dfc22bd5e00", 12820119020, "model 573152、version 3112728"},
	{"qwen-image-edit-rapid-aio-nsfw-v23", "Qwen-Rapid-AIO-NSFW-v23.safetensors", "https://huggingface.co/Phr00t/Qwen-Image-Edit-Rapid-AIO/resolve/691024f438640508f8aa86414863fc15edfb8a84/v23/Qwen-Rapid-AIO-NSFW-v23.safetensors", "fdb919fc81bea63f13759967fc92c9118142e5c70d4e6795199233a35eefa233", 28431840023, ""},
	{"qwen-image-21-vnccs-pose", "VNCCS_QI2_PoseStudioV1.1.safetensors", "https://huggingface.co/MIUProject/VNCCS_PoseStudio_QI2.1/resolve/9857b8a256655da446772a3f1f2b033e33181a6d/VNCCS_QI2_PoseStudioV1.1.safetensors", "d03c0ca3dd47c0de4c26c9c56562d12e7c968442c9decbb14d4b16bd5db3843d", 159436408, ""},
}

func TestAddedModelsArePinnedToTheFileHuggingFaceReports(t *testing.T) {
	c := catalog.Default()
	for _, p := range addedPins {
		m, ok := c.Model(p.id)
		require.True(t, ok, p.id)
		var f catalog.File
		for _, file := range m.Files {
			if file.Name == p.name {
				f = file
			}
		}
		require.Equal(t, p.name, f.Name, p.id)
		assert.Equal(t, p.url, f.URL, p.id)
		assert.Equal(t, p.sha, f.SHA256, p.id)
		assert.Equal(t, p.size, f.Size, p.id)
		if p.civitai != "" {
			assert.Contains(t, f.SourceNote, "Civitai", p.id)
			assert.Contains(t, f.SourceNote, p.civitai, p.id)
			assert.Contains(t, f.SourceNote, "SHA-256 が一致", p.id)
			assert.Contains(t, f.SourceNote, "2026-11-03", p.id)
		}
	}
}

// The adult ones are advanced, consent-required, and say photoreal or anime in the first words; the VRAM is an estimate.
func TestAddedAdultModelsAreAdvancedConsentRequiredAndSayWhatKindTheyAre(t *testing.T) {
	c := catalog.Default()
	for id, prefix := range map[string]string{
		"realvisxl-v50-lightning":            "【実写】",
		"lustify-v10-krea2-turbo-fp8":        "【実写】",
		"qwen-image-edit-rapid-aio-nsfw-v23": "【実写寄り・編集】",
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		assert.True(t, m.Adult(), id)
		assert.Equal(t, catalog.TierAdvanced, m.Tier, id)
		assert.True(t, m.ConsentRequired, id)
		assert.True(t, m.VRAM.Estimate, id)
		assert.True(t, strings.HasPrefix(m.Description, prefix), id+": "+m.Description)
	}
}

// What each license says. Lightning is the author's openrail++ (商用OK); LUSTIFY v10 is built on Krea 2, whose license is conditional on the
// revenue of the company, and the author of LUSTIFY says nothing, so the whole answer is 不明; Rapid-AIO is the author's apache-2.0 (the
// merged LoRAs' own licenses are not stated, and the notice says so); the VNCCS LoRA says nothing, and the Qwen base makes the answer 商用NG.
func TestAddedModelsLicensesAreOnlyWhatTheSourcesSay(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string]struct {
		license string
		answer  modellicense.Commercial
		source  string
		part    map[string]modellicense.Commercial // by the start of the part's name
	}{
		"realvisxl-v50-lightning":            {"creativeml-openrail++-m", modellicense.Yes, "huggingface.co/SG161222/RealVisXL_V5.0_Lightning/blob/", nil},
		"lustify-v10-krea2-turbo-fp8":        {"krea-2-community", modellicense.Unknown, "huggingface.co/Comfy-Org/Krea-2/blob/588feaf955b08b9ac4723eeb751752ab045dba71/LICENSE.pdf", map[string]modellicense.Commercial{"Krea 2": modellicense.Conditional, "ライセンスの記載なし": modellicense.Unknown}},
		"qwen-image-edit-rapid-aio-nsfw-v23": {"apache-2.0", modellicense.Yes, "huggingface.co/Phr00t/Qwen-Image-Edit-Rapid-AIO/blob/", nil},
		"qwen-image-21-vnccs-pose":           {"qwen-research", modellicense.No, "huggingface.co/Qwen/Qwen-Image-2.1/blob/", map[string]modellicense.Commercial{"ライセンスの記載なし": modellicense.Unknown}},
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		l := m.License
		assert.Equal(t, want.license, l.ID, id)
		assert.Equal(t, want.answer, l.Commercial, id)
		assert.Contains(t, l.URL, want.source, id)
		assert.Equal(t, "2026-11-03", l.CheckedAt, id)
		if l.Commercial != modellicense.Yes || id == "qwen-image-edit-rapid-aio-nsfw-v23" {
			assert.NotEmpty(t, l.Notice, id)
		}
		for start, answer := range want.part {
			found := false
			for _, part := range l.Components {
				if strings.HasPrefix(part.Name, start) {
					found = true
					assert.Equal(t, answer, part.Commercial, id+": "+part.Part)
				}
			}
			assert.True(t, found, id+": a part named "+start)
		}
	}
}

// The Lightning checkpoint gets its own templates: the author asks for 5 steps, cfg 1 to 2 and DPM++ SDE, which the SDXL template (25 steps,
// cfg 7) would break. The workflows load the Lightning checkpoint and not the one of the template.
func TestRealVisLightningUsesTheLightningTemplates(t *testing.T) {
	m, _ := catalog.Default().Model("realvisxl-v50-lightning")
	require.Len(t, m.Workflows, 2)
	assert.Equal(t, []string{"txt2img-sdxl-lightning", "img2img-sdxl-lightning"}, []string{m.Workflows[0].Template, m.Workflows[1].Template})
	assert.Equal(t, "realvisxl-v50-lightning-img2img", m.Workflows[1].Name)
	for _, w := range m.Workflows {
		got, err := w.Model(localcfg.TemplateByID)
		require.NoError(t, err, w.Name)
		assert.Equal(t, "RealVisXL_V5.0_Lightning_fp16.safetensors", checkpointOf(t, got), w.Name)
		g, err := localcfg.ParseGraph(got.Workflow.Graph)
		require.NoError(t, err)
		ks := g["3"]
		assert.JSONEq(t, `5`, string(ks.Inputs["steps"]), w.Name)
		assert.JSONEq(t, `1.5`, string(ks.Inputs["cfg"]), w.Name)
		assert.JSONEq(t, `"dpmpp_sde"`, string(ks.Inputs["sampler_name"]), w.Name)
		assert.JSONEq(t, `"karras"`, string(ks.Inputs["scheduler"]), w.Name)
	}
}

// The files of LUSTIFY v10 that Krea-2 shares with the official repackaging: the VAE is qwen-image's file (installed once), and the text
// encoder is the Comfy-Org repackaging at the commit of the license.
func TestLustifyV10SharesTheVAEWithQwenImage(t *testing.T) {
	c := catalog.Default()
	m, _ := c.Model("lustify-v10-krea2-turbo-fp8")
	qwen, _ := c.Model("qwen-image")
	require.Len(t, m.Files, 3)
	assert.Equal(t, "models/diffusion_models/lustifyNSFWCheckpoint_v10Krea2.safetensors", m.Files[0].Path())
	assert.Equal(t, "models/text_encoders/qwen3vl_4b_fp8_scaled.safetensors", m.Files[1].Path())
	assert.Contains(t, m.Files[1].URL, "Comfy-Org/Krea-2/resolve/588feaf955b08b9ac4723eeb751752ab045dba71/")
	assert.Equal(t, qwen.Files[2], m.Files[2], "the VAE is the one of qwen-image")
}
