package uncensoredvideotest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

var mediumAndLarge = map[string]struct {
	params float64
	tier   string
	inputs []traits.Input
	not    []traits.Input
	speed  string
}{
	"wan22-i2v-r1-v2-uncensored":    {14, "中（〜14B）", []traits.Input{traits.I2V, traits.FirstFrame}, []traits.Input{traits.T2V}, "fast"},
	"wan22-rapid-mega-nsfw-t2v":     {14, "中（〜14B）", []traits.Input{traits.T2V, traits.I2V, traits.FirstFrame, traits.LastFrame}, nil, "fast"},
	"wan22-t2v-14b-mystic-xxx-lora": {14, "中（〜14B）", []traits.Input{traits.T2V}, []traits.Input{traits.I2V, traits.FirstFrame}, "slow"},
	"hunyuanvideo-t2v-nsfw-lora":    {13, "大（品質重視）", []traits.Input{traits.T2V}, []traits.Input{traits.I2V, traits.FirstFrame}, "slow"},
}

// The medium and the large models: adult, uncensored, advanced, not gated, a parameter count and a speed guide, the inputs that follow from the
// workflow, no sound, and a description that says the tier. The VRAM is an estimate that says it was not run.
func TestMediumAndLargeModelsAreUncensoredAdultVideoModels(t *testing.T) {
	for id, tc := range mediumAndLarge {
		m := model(t, id)
		assert.Equal(t, "video", string(m.Kind), id)
		assert.True(t, m.Adult(), id)
		assert.True(t, m.Traits.Uncensored, id)
		assert.Equal(t, catalog.TierAdvanced, m.Tier, id)
		assert.True(t, m.ConsentRequired, id)
		assert.False(t, m.RequiresHFToken, id)
		assert.InDelta(t, tc.params, m.Traits.ParamsB, 1e-9, id)
		assert.Greater(t, m.Traits.ParamsB, 5.0, id+": above the small band")
		assert.Equal(t, tc.speed, string(m.Traits.Speed), id)
		assert.Equal(t, 16, m.VRAM.MinGB, id+": the 16 GB GPU is the target")
		assert.True(t, m.VRAM.Estimate, id)
		assert.Contains(t, m.VRAM.Source, "未確認", id)
		d := m.Derived()
		for _, in := range tc.inputs {
			assert.True(t, d.Has(in), id+" has "+string(in))
		}
		for _, in := range tc.not {
			assert.False(t, d.Has(in), id+" has not "+string(in))
		}
		assert.False(t, d.Audio, id)
		assert.Contains(t, m.Description, tc.tier, id)
		assert.Contains(t, m.Description, "音声なし", id)
		assert.Contains(t, m.Description, "不明", id+": the license is unknown and the description says so")
	}
}

// Every file is pinned to the commit and the SHA-256 and size Hugging Face reports (the LFS oid of the file at the commit; checked on 2026-11-03).
func TestMediumAndLargeFilesArePinned(t *testing.T) {
	for _, p := range []struct {
		id, name, folder, url, sha string
		size                       int64
	}{
		{"wan22-i2v-r1-v2-uncensored", "Wan2.2_I2V_High_R1_n54w_v2.safetensors", "diffusion_models", "https://huggingface.co/rzgar/Wan2.2_I2V_R1_base_v2/resolve/2118ff4037ab196834f6d36185a197ef3a7a3754/Wan2.2_I2V_High_R1_n54w_v2.safetensors",
			"2e048f2e0977dce05098886ca1d171569c622897e94fc549b433f64ed98a13f4", 14803879958},
		{"wan22-i2v-r1-v2-uncensored", "Wan2.2_I2V_Low_R1_n54w_v2.safetensors", "diffusion_models", "https://huggingface.co/rzgar/Wan2.2_I2V_R1_base_v2/resolve/2118ff4037ab196834f6d36185a197ef3a7a3754/Wan2.2_I2V_Low_R1_n54w_v2.safetensors",
			"08fe6e276550d80928f9d016a7a9c67f3aa6444aff4d92a52d1879fefe586e7c", 14803879958},
		{"wan22-rapid-mega-nsfw-t2v", "wan2.2-rapid-mega-aio-nsfw-v12.2.safetensors", "checkpoints", "https://huggingface.co/Phr00t/WAN2.2-14B-Rapid-AllInOne/resolve/30e803babc766d791f140b197aa7bf7f18f6e36d/Mega-v12/wan2.2-rapid-mega-aio-nsfw-v12.2.safetensors",
			"97c9f3f993c463e22497a6f85bc428dda047bd2d381383a8e77e26ded6312b1e", 23284018448},
		{"wan22-t2v-14b-mystic-xxx-lora", "wan2.2_t2v_high_noise_14B_fp8_scaled.safetensors", "diffusion_models", "https://huggingface.co/Comfy-Org/Wan-2.2/resolve/ee6f4a40737a995bf5818954cfce6d59443b0f04/split_files/diffusion_models/wan2.2_t2v_high_noise_14B_fp8_scaled.safetensors",
			"cad711ae211c8b23455ec68cd6a190a33a3d874234a77eb57266d73f8f0e6c9f", 14293923632},
		{"wan22-t2v-14b-mystic-xxx-lora", "wan2.2_t2v_low_noise_14B_fp8_scaled.safetensors", "diffusion_models", "https://huggingface.co/Comfy-Org/Wan-2.2/resolve/ee6f4a40737a995bf5818954cfce6d59443b0f04/split_files/diffusion_models/wan2.2_t2v_low_noise_14B_fp8_scaled.safetensors",
			"e71b96d7c82e638694c5e7fb98fac4bfb0e4ddc5fbbb4b1df40da8f0f1278a97", 14293923632},
		{"wan22-t2v-14b-mystic-xxx-lora", "Wan2.2-High-MysticXXX.safetensors", "loras", "https://huggingface.co/yeqiu168182/Wan2.2-High-MysticXXX/resolve/b363a6f321c9704844af20813c99274a125fa1a5/Wan2.2-High-MysticXXX.safetensors",
			"167a2a05fe983f522886f3692611caee562ebfd7e8ee5b2c610c7a7a5b1ffa7d", 306807976},
		{"wan22-t2v-14b-mystic-xxx-lora", "Wan2.2-Low-MysticXXX.safetensors", "loras", "https://huggingface.co/yeqiu168182/Wan2.2-Low-MysticXXX/resolve/f8e7308af20fb46a5c0f6619e4b35f3ed1ab4ee9/Wan2.2-Low-MysticXXX.safetensors",
			"e8509a756b9d404e8b1c0efc72f0f4a9ea2bb20c0435a300d41873e92b0bf5fb", 306807976},
		{"hunyuanvideo-t2v-nsfw-lora", "hunyuan_video_t2v_720p_bf16.safetensors", "diffusion_models", "https://huggingface.co/Comfy-Org/HunyuanVideo_repackaged/resolve/533a43813f01517d9e624f9e824a85bb94094120/split_files/diffusion_models/hunyuan_video_t2v_720p_bf16.safetensors",
			"c6ff2d107f0fec571fe276ad847468404ed01855c28c0be8859c3b311daec52a", 25642131432},
		{"hunyuanvideo-t2v-nsfw-lora", "llava_llama3_fp8_scaled.safetensors", "text_encoders", "https://huggingface.co/Comfy-Org/HunyuanVideo_repackaged/resolve/533a43813f01517d9e624f9e824a85bb94094120/split_files/text_encoders/llava_llama3_fp8_scaled.safetensors",
			"2f0c3ad255c282cead3f078753af37d19099cafcfc8265bbbd511f133e7af250", 9091392483},
		{"hunyuanvideo-t2v-nsfw-lora", "hunyuan_video_nsfw_e7.safetensors", "loras", "https://huggingface.co/BigStationW/HunyuanVideo_nfsw_lora/resolve/e723e5d9279299457d9676882d1522dfe0832171/nsfw-e7.safetensors",
			"d70f5ecd2efea7653f8ad2a4a4262a10d91ec6119bf055c22c7343fcfe4fc77b", 322522040},
	} {
		f := file(t, model(t, p.id), p.name)
		assert.Equal(t, p.folder, f.Folder, p.name)
		assert.Equal(t, p.url, f.URL, p.name)
		assert.Equal(t, p.sha, f.SHA256, p.name)
		assert.Equal(t, p.size, f.Size, p.name)
	}
}

// None of the four is known to be commercial: each license says why, the strictest part is shown, the source is dated, and the models that carry
// a mirrored file name its origin. Phr00t's merge says that the author stopped maintaining it.
func TestMediumAndLargeLicensesAreUnknownWithTheirReasons(t *testing.T) {
	for id, reason := range map[string]string{
		"wan22-i2v-r1-v2-uncensored":    "焼き込んだ非検閲の LoRA",
		"wan22-rapid-mega-nsfw-t2v":     "多数の成人向け LoRA",
		"wan22-t2v-14b-mystic-xxx-lora": "HF 側に本文が無い",
		"hunyuanvideo-t2v-nsfw-lora":    "license: unknown",
	} {
		m := model(t, id)
		assert.Equal(t, modellicense.Unknown, m.License.Commercial, id)
		assert.Contains(t, m.License.Summary, reason, id)
		assert.NoError(t, m.License.Info().Check(), id)
		assert.Equal(t, "2026-11-04", m.License.CheckedAt, id)
		assert.Contains(t, m.License.Notice, "商用には使わないでください", id)
		require.GreaterOrEqual(t, len(m.License.Components), 2, id)
	}
	mega := model(t, "wan22-rapid-mega-nsfw-t2v")
	assert.Contains(t, mega.Description, "メンテ", "the author stopped maintaining it")
	assert.Contains(t, mega.Description, "deprecated")
	assert.Contains(t, mega.License.Notice, "メンテを終了")

	hunyuan := model(t, "hunyuanvideo-t2v-nsfw-lora")
	assert.Equal(t, []string{"EU", "GB", "KR"}, hunyuan.License.ExcludedRegions, "the Tencent Hunyuan Community License does not apply in the EU, the UK and South Korea")
	assert.Equal(t, modellicense.Conditional, hunyuan.License.Components[0].Commercial)
	assert.Equal(t, modellicense.Unknown, hunyuan.License.Components[1].Commercial)

	mystic := model(t, "wan22-t2v-14b-mystic-xxx-lora")
	for _, name := range []string{"Wan2.2-High-MysticXXX.safetensors", "Wan2.2-Low-MysticXXX.safetensors"} {
		f := file(t, mystic, name)
		assert.Contains(t, f.SourceNote, "model 1295758", name)
		assert.Contains(t, f.SourceNote, "2026-11-03", name)
	}
	for _, f := range mystic.Files {
		if f.Folder != "loras" {
			assert.Empty(t, f.SourceNote, f.Name)
		}
	}
	assert.Equal(t, []string{"nsfwsks"}, hunyuan.TriggerWords)
	assert.Contains(t, hunyuan.Description, "nsfwsks")
}

// The Wan 2.2 14B models share the text encoder and the VAE of the Wan 2.2 base; the T2V LoRA model carries its own T2V base (not the I2V one);
// the Mega merge is a single checkpoint with the text encoder and the VAE inside; the large model is HunyuanVideo with the standard text encoders.
func TestMediumAndLargeFileSets(t *testing.T) {
	c := catalog.Default()
	i2v, _ := c.Model("wan22-i2v-14b")
	r1 := model(t, "wan22-i2v-r1-v2-uncensored")
	assert.Len(t, r1.Files, 4)
	assert.Equal(t, i2v.Files[2:], r1.Files[2:], "the text encoder and the VAE of the Wan 2.2 base")

	mystic := model(t, "wan22-t2v-14b-mystic-xxx-lora")
	assert.Len(t, mystic.Files, 6)
	assert.Contains(t, mystic.Files, file(t, i2v, "umt5_xxl_fp8_e4m3fn_scaled.safetensors"))
	assert.Contains(t, mystic.Files, file(t, i2v, "wan_2.1_vae.safetensors"))
	for _, f := range mystic.Files[:2] {
		assert.Contains(t, f.Name, "_t2v_", "the text to video experts, not the first-frame ones")
	}

	mega := model(t, "wan22-rapid-mega-nsfw-t2v")
	require.Len(t, mega.Files, 1)
	assert.Equal(t, "checkpoints", mega.Files[0].Folder)

	hv := model(t, "hunyuanvideo-t2v-nsfw-lora")
	var names []string
	for _, f := range hv.Files {
		names = append(names, f.Folder+"/"+f.Name)
	}
	assert.Equal(t, []string{"diffusion_models/hunyuan_video_t2v_720p_bf16.safetensors", "text_encoders/clip_l.safetensors", "text_encoders/llava_llama3_fp8_scaled.safetensors", "vae/hunyuan_video_vae_bf16.safetensors", "loras/hunyuan_video_nsfw_e7.safetensors"}, names)
	assert.Equal(t, 24, hv.VRAM.RecommendedGB)
	assert.NotEmpty(t, hv.VRAM.Limits, "a GPU between 16 GB and the recommendation runs it with limits, and the limits are said")
}

// The macOS column (#979) is filled only where a primary source reports a failure: the models that load the fp8 UMT5 file (ComfyUI #10292). The
// Mega checkpoint and HunyuanVideo load other files that no report names, so they stay without the column, and their descriptions say it is unconfirmed.
func TestMacOSColumnFollowsTheReports(t *testing.T) {
	for _, id := range []string{"wan22-i2v-r1-v2-uncensored", "wan22-t2v-14b-mystic-xxx-lora", "nsfw-wan21-1b3-t2v", "nsfw-anime-wan22-5b", "wan22-ti2v-5b-cowgirl-lora"} {
		require.NotNil(t, model(t, id).Unsupported, id)
	}
	for _, id := range []string{"wan22-rapid-mega-nsfw-t2v", "hunyuanvideo-t2v-nsfw-lora"} {
		m := model(t, id)
		assert.Nil(t, m.Unsupported, id)
		assert.Contains(t, m.Description, "macOS（Apple Silicon）は未確認", id)
	}
}

// The workflows of the four models.
func TestMediumAndLargeWorkflows(t *testing.T) {
	for id, want := range map[string]map[string]string{
		"wan22-i2v-r1-v2-uncensored":    {"wan22-i2v-r1-v2-uncensored": "img2video-wan22-14b-rzgar-r1"},
		"wan22-rapid-mega-nsfw-t2v":     {"wan22-rapid-mega-nsfw-t2v": "txt2video-wan22-14b-rapid-mega", "wan22-rapid-mega-nsfw-i2v": "img2video-wan22-14b-rapid-mega", "wan22-rapid-mega-nsfw-flf2v": "flf2v-wan22-14b-rapid-mega"},
		"wan22-t2v-14b-mystic-xxx-lora": {"wan22-t2v-14b-mystic-xxx-lora": "txt2video-wan22-14b-lora"},
		"hunyuanvideo-t2v-nsfw-lora":    {"hunyuanvideo-t2v-nsfw-lora": "txt2video-hunyuan-video-lora"},
	} {
		got := map[string]string{}
		for _, w := range model(t, id).Workflows {
			got[w.Name] = w.Template
		}
		assert.Equal(t, want, got, id)
	}
}
