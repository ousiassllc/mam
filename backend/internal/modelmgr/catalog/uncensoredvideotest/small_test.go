// Package uncensoredvideotest holds the tests of the uncensored video models that Issue #1012 added to the catalog, tier by tier (small: up to 5B).
package uncensoredvideotest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

func model(t *testing.T, id string) catalog.Model {
	t.Helper()
	m, ok := catalog.Default().Model(id)
	require.True(t, ok, id)
	return m
}

func file(t *testing.T, m catalog.Model, name string) catalog.File {
	t.Helper()
	for _, f := range m.Files {
		if f.Name == name {
			return f
		}
	}
	require.Failf(t, "no such file", "%s has no %s", m.ID, name)
	return catalog.File{}
}

// The small tier (up to 5B): each model is an adult, uncensored, advanced model with its parameter count in the band "small", and its inputs follow
// from the workflows (the 1.3B model is text to video only; the 5B models also take a first frame). Nothing makes sound.
func TestSmallTierModelsAreUncensoredAdultVideoModelsWithTheirInputs(t *testing.T) {
	for id, tc := range map[string]struct {
		params float64
		label  string
		inputs []traits.Input
		not    []traits.Input
	}{
		"nsfw-wan21-1b3-t2v":         {1.3, "1.3B", []traits.Input{traits.T2V}, []traits.Input{traits.I2V, traits.FirstFrame}},
		"nsfw-anime-wan22-5b":        {5, "5B", []traits.Input{traits.T2V, traits.I2V, traits.FirstFrame}, []traits.Input{traits.LastFrame}},
		"wan22-ti2v-5b-cowgirl-lora": {5, "5B", []traits.Input{traits.T2V, traits.I2V, traits.FirstFrame}, []traits.Input{traits.LastFrame}},
	} {
		m := model(t, id)
		assert.Equal(t, "video", string(m.Kind), id)
		assert.True(t, m.Adult(), id)
		assert.True(t, m.Traits.Uncensored, id)
		assert.Equal(t, catalog.TierAdvanced, m.Tier, id)
		assert.True(t, m.ConsentRequired, id)
		assert.False(t, m.RequiresHFToken, id, "not gated")
		assert.InDelta(t, tc.params, m.Traits.ParamsB, 1e-9, id)
		assert.Equal(t, tc.label, m.Traits.ParamsLabel, id)
		assert.LessOrEqual(t, m.Traits.ParamsB, 5.0, id+": small is up to 5B")
		assert.LessOrEqual(t, m.VRAM.MinGB, 16, id+": fits a 16 GB GPU")
		assert.True(t, m.VRAM.Estimate, id+": nothing was run on a GPU")
		assert.Contains(t, m.VRAM.Source, "未確認", id)
		d := m.Derived()
		for _, in := range tc.inputs {
			assert.True(t, d.Has(in), id+" has "+string(in))
		}
		for _, in := range tc.not {
			assert.False(t, d.Has(in), id+" has not "+string(in))
		}
		assert.False(t, d.Audio, id)
		assert.Contains(t, m.Description, "小（〜5B）", id+": the description says the tier")
		assert.Contains(t, m.Description, "音声なし", id)
	}
}

// Every file is pinned to the commit and the SHA-256 and size that Hugging Face reports (the LFS oid of the file at the commit, the X-Linked-Etag of
// the resolve URL; checked on 2026-11-03), and the downloads of the two diffusion models and the LoRA were hashed with sha256sum.
func TestSmallTierFilesArePinnedToWhatHuggingFaceReports(t *testing.T) {
	for _, p := range []struct {
		id, name, folder, url, sha string
		size                       int64
	}{
		{"nsfw-wan21-1b3-t2v", "nsfw_wan_1.3b_exp_e14.safetensors", "diffusion_models", "https://huggingface.co/NSFW-API/NSFW_Wan_1.3b/resolve/4d8db25317335b4a0cfbefad9877b893f08dbc67/wan_1.3B_exp_e14.safetensors",
			"86daed5f43a68d7be2ca1f0e85d2ac51f3ba7fb735c76fd9574323e18fe40c6c", 2838077328},
		{"nsfw-anime-wan22-5b", "nsfw_anime_wan_5b_3.0pro.safetensors", "diffusion_models", "https://huggingface.co/yaleiyaleichiling/NSFW-Anime-wan-5B/resolve/99b77d1705e88bfe02c80e88c349f5c1d89ce93b/3.0Pro.safetensors",
			"69299be08b667a6a8b6d1a02727afd8e31a87a47daf508e1addec6b6dab83acf", 9999658848},
		{"wan22-ti2v-5b-cowgirl-lora", "c0wg1rl_reverse_c0wg1rl_wan22_5b_ti2v_e380.safetensors", "loras", "https://huggingface.co/UnifiedHorusRA/WAN_C0WG1RL_REV3RSE_C0WG1RL_-_T2V_I2V_LoRa/resolve/d0ecbc742146d8c23aba9c040ec06d301ecd53ec/c0wg1rl.3_wan22-5b-ti2v%20-%20e380.safetensors",
			"65275f02cd8c0aaee184a583d1e64264c0dba2725d759647cd3e03f578796c94", 161292752},
	} {
		f := file(t, model(t, p.id), p.name)
		assert.Equal(t, p.folder, f.Folder, p.id)
		assert.Equal(t, p.url, f.URL, p.id)
		assert.Equal(t, p.sha, f.SHA256, p.id)
		assert.Equal(t, p.size, f.Size, p.id)
	}
}

// The text encoder and the VAE of the small models are the very files of the Wan 2.2 5B (and the Wan 2.1 VAE of the 14B ones): one copy on disk. The
// LoRA model carries the whole fp8 5B base, so the base can be deleted while the LoRA model stays.
func TestSmallTierSharesTheWanTextEncoderAndTheVAE(t *testing.T) {
	c := catalog.Default()
	fp8, _ := c.Model("wan22-ti2v-5b-fp8")
	lora := model(t, "wan22-ti2v-5b-cowgirl-lora")
	for _, f := range fp8.Files {
		assert.Contains(t, lora.Files, f, "the LoRA model carries "+f.Name)
	}
	assert.Len(t, lora.Files, len(fp8.Files)+1)

	anime := model(t, "nsfw-anime-wan22-5b")
	fp16, _ := c.Model("wan22-ti2v-5b")
	assert.Equal(t, fp16.Files[1:], anime.Files[1:], "the text encoder and the VAE of the fp16 5B")
	wan13 := model(t, "nsfw-wan21-1b3-t2v")
	i2v14, _ := c.Model("wan22-i2v-14b")
	assert.Contains(t, wan13.Files, file(t, i2v14, "umt5_xxl_fp8_e4m3fn_scaled.safetensors"))
	assert.Contains(t, wan13.Files, file(t, i2v14, "wan_2.1_vae.safetensors"))
}

// The licenses: none of the three is known to be commercial. The 1.3B card only declares OpenRAIL-M (no text, training data from scraped posts), the
// anime model has the Apache-2.0 text but no word on the rights of its training data (and the repository was restored), the LoRA has no license at all
// (Civitai's original allows RentCivit and generated images only). Each says why in its summary, and the stricter part is what is shown.
func TestSmallTierLicensesAreUnknownWithTheirReasons(t *testing.T) {
	for id, tc := range map[string]struct {
		license string
		reason  string
	}{
		"nsfw-wan21-1b3-t2v":         {"creativeml-openrail-m-declared", "Reddit"},
		"nsfw-anime-wan22-5b":        {"apache-2.0", "復元"},
		"wan22-ti2v-5b-cowgirl-lora": {"unspecified", "RentCivit"},
	} {
		m := model(t, id)
		assert.Equal(t, tc.license, m.License.ID, id)
		assert.Equal(t, modellicense.Unknown, m.License.Commercial, id)
		assert.Contains(t, m.License.Summary+m.Description, tc.reason, id)
		assert.NoError(t, m.License.Info().Check(), id)
		require.GreaterOrEqual(t, len(m.License.Components), 2, id)
		assert.Equal(t, modellicense.Yes, m.License.Components[0].Commercial, id+": the base model is Apache-2.0")
		assert.Contains(t, m.License.Notice, "商用には使わないでください", id)
		assert.Equal(t, "2026-11-03", m.License.CheckedAt, id)
	}
}

// The LoRA is a third party's mirror of the Civitai original: the file says where the original is and when the SHA-256 was compared, its
// trigger words are the author's, and the model is the only one with a LoRA file.
func TestCowgirlLoRAIsAMirrorWithTheAuthorsTriggerWords(t *testing.T) {
	m := model(t, "wan22-ti2v-5b-cowgirl-lora")
	assert.Equal(t, []string{"r3v3rs3_c0wg1rl", "c0wg1rl"}, m.TriggerWords)
	f := file(t, m, "c0wg1rl_reverse_c0wg1rl_wan22_5b_ti2v_e380.safetensors")
	assert.Contains(t, f.SourceNote, "model 1428098")
	assert.Contains(t, f.SourceNote, "2026-11-03")
	for _, other := range m.Files {
		if other.Folder != "loras" {
			assert.Empty(t, other.SourceNote, other.Name+" is not a mirror")
		}
	}
	for _, id := range []string{"nsfw-wan21-1b3-t2v", "nsfw-anime-wan22-5b"} {
		assert.Empty(t, model(t, id).TriggerWords, id)
	}
}

// The workflows of the models are the templates built for them (the structure is checked in templatestest).
func TestSmallTierWorkflows(t *testing.T) {
	for id, want := range map[string]map[string]string{
		"nsfw-wan21-1b3-t2v":         {"nsfw-wan21-1b3-t2v": "txt2video-wan21-1b3-nsfw"},
		"nsfw-anime-wan22-5b":        {"nsfw-anime-wan22-5b": "txt2video-wan22-5b-nsfw-anime", "nsfw-anime-wan22-5b-i2v": "img2video-wan22-5b-nsfw-anime"},
		"wan22-ti2v-5b-cowgirl-lora": {"wan22-ti2v-5b-cowgirl-lora": "txt2video-wan22-5b-lora", "wan22-ti2v-5b-cowgirl-lora-i2v": "img2video-wan22-5b-lora"},
	} {
		got := map[string]string{}
		for _, w := range model(t, id).Workflows {
			got[w.Name] = w.Template
		}
		assert.Equal(t, want, got, id)
	}
}
