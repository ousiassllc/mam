package catalog_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

var adultIDs = []string{"chroma1-hd", "flux1-dev-nsfw-master", "noctq-v4-qwen-image21", "wan22-i2v-14b-nsfw-lora", "wan22-remix-nsfw-i2v"}

// The models of Issue #841 and which of them are for adults: the uncensored models and the LoRAs that make a model
// uncensored. wan22-i2v-14b is the official base (no NFAA tag on Hugging Face), so it is a general standard model.
func TestUncensoredModelsAreAdvancedAdultModels(t *testing.T) {
	c := catalog.Default()
	var adult []string
	for _, m := range c.Models {
		if m.Adult() {
			adult = append(adult, m.ID)
			assert.Equal(t, catalog.TierAdvanced, m.Tier, m.ID+": an adult model needs the consent")
			assert.True(t, m.ConsentRequired, m.ID)
		} else {
			assert.Equal(t, catalog.RatingGeneral, m.ContentRating, m.ID+": every model says its rating")
		}
		assert.True(t, m.VRAM.Estimate || m.ContentRating == catalog.RatingGeneral && m.VRAM.Source != "", m.ID)
	}
	slices.Sort(adult)
	assert.Equal(t, adultIDs, adult)
	base, _ := c.Model("wan22-i2v-14b")
	assert.Equal(t, catalog.RatingGeneral, base.ContentRating)
	assert.Equal(t, catalog.TierStandard, base.Tier)

	for id, trigger := range map[string][]string{"flux1-dev-nsfw-master": {"NSFW", "Nude"}, "wan22-i2v-14b-nsfw-lora": {"nsfwsks"}, "chroma1-hd": nil} {
		m, _ := c.Model(id)
		assert.Equal(t, trigger, m.TriggerWords, id)
	}
	for _, id := range adultIDs {
		m, _ := c.Model(id)
		assert.True(t, m.VRAM.Estimate, id+": nothing was run on a GPU")
	}
}

// Every file of the new models is pinned to the commit and the SHA-256 and size that Hugging Face reports (the LFS oid of
// the file at the commit; checked on 2026-10-25 against the tree API and the X-Linked-Etag of the resolve URL).
func TestUncensoredModelFilesArePinnedToWhatHuggingFaceReports(t *testing.T) {
	pins := [][6]any{
		{"diffusion_models/NoctQ_V4_int8_convrot.safetensors", "Noctaluna/Noct-Q-Uncensored-Qwen-Image-2.1", "a81b9af51120a78e285e57906f2250a2a02080e9", "NoctQ_V4_int8_convrot.safetensors", "4d92d5538253ab36e6a73b056cce5f697950f4303cb9e6686a3198d0ea13f9e8", int64(7256784368)},
		{"text_encoders/qwen3vl_8b_int8_convrot.safetensors", "Comfy-Org/Qwen-Image-2.1", "0217efd1d9c9452e84f6448903d6e21bfa171ffd", "text_encoders/qwen3vl_8b_int8_convrot.safetensors", "8bfd0f6e12abf2d2d697ecc888e5e90b0d6741d6708f05799f53afa560452e8f", int64(9350798360)},
		{"vae/qwen_image_2.1_vae_bf16.safetensors", "Comfy-Org/Qwen-Image-2.1", "0217efd1d9c9452e84f6448903d6e21bfa171ffd", "vae/qwen_image_2.1_vae_bf16.safetensors", "bb21f7473051e1ac368515dd3f2e15cd44d7a11748ee8823e1ddca3e4876b7c9", int64(675509688)},
		{"diffusion_models/Chroma1-HD-fp8mixed.safetensors", "Comfy-Org/Chroma1-HD", "409cfff960d66598d4d0ff46cb932f53492cc6d9", "split_files/diffusion_models/Chroma1-HD-fp8mixed.safetensors", "a2928ca6075f308f4d5e2182e2b96120fa8ad270ec6ea9b1b5c724c85c49a575", int64(9193379316)},
		{"text_encoders/t5xxl_fp8_e4m3fn_scaled.safetensors", "comfyanonymous/flux_text_encoders", "6af2a98e3f615bdfa612fbd85da93d1ed5f69ef5", "t5xxl_fp8_e4m3fn_scaled.safetensors", "a498f0485dc9536735258018417c3fd7758dc3bccc0a645feaa472b34955557a", int64(5157348688)},
		{"vae/ae.safetensors", "lodestones/Chroma", "2f3b2730d7b5edbd02cdff12f72610af5787300b", "ae.safetensors", "afc8e28272cd15db3919bacdb6918ce9c1ed22e96cb12c4d5ed0fba823529e38", int64(335304388)},
		{"checkpoints/flux1-dev-fp8.safetensors", "Comfy-Org/Flux-1-Dev", "83c446ef27a6ac1e9e36ecf13257283aa12cf22a", "flux1-dev-fp8.safetensors", "8e91b68084b53a7fc44ed2a3756d821e355ac1a7b6fe29be760c1db532f3d88a", int64(17246524772)},
		{"loras/NSFW_master_Flux.safetensors", "Keltezaa/NSFW_MASTER_FLUX", "987923895d4019f16362652a4f3a019c5a6868e4", "NSFW_master_Flux.safetensors", "84ce473cdeaa1ddd016cdf0406e08b43bb14c01d2c94ef48da2a035bb0b925c2", int64(171969344)},
		{"diffusion_models/wan2.2_i2v_high_noise_14B_fp8_scaled.safetensors", "Comfy-Org/Wan-2.2", "ee6f4a40737a995bf5818954cfce6d59443b0f04", "split_files/diffusion_models/wan2.2_i2v_high_noise_14B_fp8_scaled.safetensors", "6122e79d55e0f235698d11d657f3b196c5273c830da00b2b013c5a048d5e6a42", int64(14294742832)},
		{"diffusion_models/wan2.2_i2v_low_noise_14B_fp8_scaled.safetensors", "Comfy-Org/Wan-2.2", "ee6f4a40737a995bf5818954cfce6d59443b0f04", "split_files/diffusion_models/wan2.2_i2v_low_noise_14B_fp8_scaled.safetensors", "5471a457b6ac404202a5fbe6c11595a3d5641fc766b00f38763f72303fffc21e", int64(14294742832)},
		{"text_encoders/umt5_xxl_fp8_e4m3fn_scaled.safetensors", "Comfy-Org/Wan-2.2", "ee6f4a40737a995bf5818954cfce6d59443b0f04", "split_files/text_encoders/umt5_xxl_fp8_e4m3fn_scaled.safetensors", "c3355d30191f1f066b26d93fba017ae9809dce6c627dda5f6a66eaa651204f68", int64(6735906897)},
		{"vae/wan_2.1_vae.safetensors", "Comfy-Org/Wan-2.2", "ee6f4a40737a995bf5818954cfce6d59443b0f04", "split_files/vae/wan_2.1_vae.safetensors", "2fc39d31359a4b0a64f55876d8ff7fa8d780956ae2cb13463b0223e15148976b", int64(253815318)},
		{"diffusion_models/Wan2.2_Remix_NSFW_i2v_14b_high_lighting_fp8_e4m3fn_v3.0.safetensors", "FX-FeiHou/wan2.2-Remix", "4369125a0f1157c6b8768df18b7e81bacd5fdc1f", "NSFW/Wan2.2_Remix_NSFW_i2v_14b_high_lighting_fp8_e4m3fn_v3.0.safetensors", "2ac60c97ca6911090cdbdf055b685af3646b7bd93c19c9aae0f86f4ea018e97d", int64(14291272136)},
		{"diffusion_models/Wan2.2_Remix_NSFW_i2v_14b_low_lighting_fp8_e4m3fn_v3.0.safetensors", "FX-FeiHou/wan2.2-Remix", "4369125a0f1157c6b8768df18b7e81bacd5fdc1f", "NSFW/Wan2.2_Remix_NSFW_i2v_14b_low_lighting_fp8_e4m3fn_v3.0.safetensors", "a239063c377e530229c4e5b1fecdcbe9de1201a30126dd42297435287e713229", int64(14291272136)},
		{"loras/NSFW-22-H-e8.safetensors", "lopi999/Wan2.2-I2V_General-NSFW-LoRA", "aeef17d7fa51d753ab7d1004ddb4f218a95d756d", "NSFW-22-H-e8.safetensors", "34e2144d3cd65360f97d09ccbe03e1c39a096df6c9234af5fe3899d1b63cda39", int64(613516752)},
		{"loras/NSFW-22-L-e8.safetensors", "lopi999/Wan2.2-I2V_General-NSFW-LoRA", "aeef17d7fa51d753ab7d1004ddb4f218a95d756d", "NSFW-22-L-e8.safetensors", "d6b783742f4d5fd63a0223ae1d5bf64fc995a6b408480ac2a00528ae0d4146db", int64(613516752)},
	}
	files := map[string]catalog.File{}
	for _, m := range catalog.Default().Models {
		for _, f := range m.Files {
			files[f.Folder+"/"+f.Name] = f
		}
	}
	for _, p := range pins {
		f, ok := files[p[0].(string)]
		require.True(t, ok, p[0])
		assert.Equal(t, fmt.Sprintf("https://huggingface.co/%s/resolve/%s/%s", p[1], p[2], p[3]), f.URL, p[0])
		assert.Equal(t, p[4], f.SHA256, p[0])
		assert.Equal(t, p[5], f.Size, p[0])
	}
}

// A file that two models share is the same file in both (same destination, hash and size), so the install fetches it once
// and the delete keeps it while a model that uses it is installed (#833).
func TestSharedFilesOfTheUncensoredModelsAreTheSameFile(t *testing.T) {
	c := catalog.Default()
	users := map[string][]string{}
	for _, m := range c.Models {
		for _, f := range m.Files {
			users[f.Path()] = append(users[f.Path()], m.ID)
		}
	}
	for path, want := range map[string][]string{
		"models/checkpoints/flux1-dev-fp8.safetensors":                             {"flux1-dev", "flux1-dev-nsfw-master"},
		"models/text_encoders/umt5_xxl_fp8_e4m3fn_scaled.safetensors":              {"wan22-i2v-14b", "wan22-i2v-14b-nsfw-lora", "wan22-remix-nsfw-i2v", "wan22-ti2v-5b"},
		"models/vae/wan_2.1_vae.safetensors":                                       {"wan22-i2v-14b", "wan22-i2v-14b-nsfw-lora", "wan22-remix-nsfw-i2v"},
		"models/diffusion_models/wan2.2_i2v_high_noise_14B_fp8_scaled.safetensors": {"wan22-i2v-14b", "wan22-i2v-14b-nsfw-lora"},
		"models/diffusion_models/wan2.2_i2v_low_noise_14B_fp8_scaled.safetensors":  {"wan22-i2v-14b", "wan22-i2v-14b-nsfw-lora"},
	} {
		got := slices.Clone(users[path])
		slices.Sort(got)
		assert.Equal(t, want, got, path)
	}
	// the LoRA-only models carry the base files too, so the base can be deleted while the LoRA model stays
	lora, _ := c.Model("wan22-i2v-14b-nsfw-lora")
	base, _ := c.Model("wan22-i2v-14b")
	for _, f := range base.Files {
		assert.Contains(t, lora.Files, f)
	}
}

// A file that a third party mirrors says where the original is and when its SHA-256 was checked to be the same; the
// models whose license has no text are not commercial use.
func TestMirroredFilesNameTheirOriginAndUnlicensedModelsAreNotCommercial(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string]map[string]string{
		"flux1-dev-nsfw-master":   {"NSFW_master_Flux.safetensors": "Civitai"},
		"wan22-remix-nsfw-i2v":    {"Wan2.2_Remix_NSFW_i2v_14b_high_lighting_fp8_e4m3fn_v3.0.safetensors": "model 2003153", "Wan2.2_Remix_NSFW_i2v_14b_low_lighting_fp8_e4m3fn_v3.0.safetensors": "model 2003153"},
		"wan22-i2v-14b-nsfw-lora": {"NSFW-22-H-e8.safetensors": "model 1307155", "NSFW-22-L-e8.safetensors": "model 1307155"},
	} {
		m, _ := c.Model(id)
		for _, f := range m.Files {
			if origin, mirrored := want[f.Name]; mirrored {
				assert.Contains(t, f.SourceNote, origin, f.Name)
				assert.Contains(t, f.SourceNote, "2026-10-25", f.Name)
			} else {
				assert.Empty(t, f.SourceNote, f.Name+" is not a mirror")
			}
		}
	}
	for _, id := range []string{"wan22-remix-nsfw-i2v", "wan22-i2v-14b-nsfw-lora"} {
		m, _ := c.Model(id)
		assert.Equal(t, "unspecified", m.License.ID, id)
		assert.False(t, m.License.CommercialUse, id)
		assert.Contains(t, m.License.Summary, "ライセンスの記載が無い", id)
		assert.Contains(t, m.License.Summary, "Civitai", id)
	}
	noct, _ := c.Model("noctq-v4-qwen-image21")
	assert.False(t, noct.License.CommercialUse)
	chroma, _ := c.Model("chroma1-hd")
	assert.True(t, chroma.License.CommercialUse, "Apache-2.0")
	assert.True(t, chroma.ConsentRequired, "but it is adult: the consent is required all the same")
}

func TestValidateChecksTheContentRatingLorasAndSourceNotes(t *testing.T) {
	find := func(c *catalog.Catalog, id string) *catalog.Model {
		i := slices.IndexFunc(c.Models, func(m catalog.Model) bool { return m.ID == id })
		require.GreaterOrEqual(t, i, 0, id)
		return &c.Models[i]
	}
	for name, tc := range map[string]struct {
		edit func(c *catalog.Catalog)
		want string
	}{
		"no rating":               {func(c *catalog.Catalog) { find(c, "sdxl-base").ContentRating = "" }, "content_rating"},
		"an unknown rating":       {func(c *catalog.Catalog) { find(c, "sdxl-base").ContentRating = "teen" }, "content_rating"},
		"an adult standard model": {func(c *catalog.Catalog) { find(c, "sdxl-base").ContentRating = catalog.RatingAdult }, "adult model is an advanced model"},
		"an unlicensed commercial model": {func(c *catalog.Catalog) {
			find(c, "wan22-remix-nsfw-i2v").License.CommercialUse = true
		}, "not commercial use"},
		"trigger words without a LoRA": {func(c *catalog.Catalog) { find(c, "sdxl-base").TriggerWords = []string{"x"} }, "trigger_words belong to a model with a LoRA"},
		"an empty trigger word":        {func(c *catalog.Catalog) { find(c, "flux1-dev-nsfw-master").TriggerWords = []string{" "} }, "trigger word is empty"},
		"a source note without a date": {func(c *catalog.Catalog) { find(c, "flux1-dev-nsfw-master").Files[1].SourceNote = "Civitai" }, "source_note must say"},
		"a LoRA the workflow does not load": {func(c *catalog.Catalog) {
			m := find(c, "flux1-dev-nsfw-master")
			m.Workflows[0].Template = "txt2img-flux-dev"
		}, "is not loaded by the workflow"},
		"a loader of a LoRA the model lacks": {func(c *catalog.Catalog) {
			m := find(c, "flux1-dev-nsfw-master")
			m.Files = m.Files[:1]
		}, "loads loras/NSFW_master_Flux.safetensors, which is not a file of the model"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}
