package lightmodelstest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// The standard (not uncensored) video models of Issue #1014: Wan 2.2 T2V 14B, Wan 2.1 T2V 1.3B, Wan 2.1 FLF2V 14B and HunyuanVideo 1.5 720p.
// Every pin is what the Hugging Face API (the LFS oid and the size) returned on 2026-11-03.
func TestStandardVideoModelsArePinnedGeneralModels(t *testing.T) {
	for id, pins := range map[string]map[string]struct {
		sha  string
		size int64
	}{
		"wan22-t2v-14b": {
			"wan2.2_t2v_high_noise_14B_fp8_scaled.safetensors": {"cad711ae211c8b23455ec68cd6a190a33a3d874234a77eb57266d73f8f0e6c9f", 14293923632},
			"wan2.2_t2v_low_noise_14B_fp8_scaled.safetensors":  {"e71b96d7c82e638694c5e7fb98fac4bfb0e4ddc5fbbb4b1df40da8f0f1278a97", 14293923632},
		},
		"wan21-t2v-1-3b": {
			"wan2.1_t2v_1.3B_fp16.safetensors": {"be531024cd9018cb5b48c40cfbb6a6191645b1c792eb8bf4f8c1c6e10f924dc5", 2838303560},
		},
		"wan21-flf2v-14b-720p": {
			"wan2.1_flf2v_720p_14B_fp8_e4m3fn.safetensors": {"d68ca694a695274e48e00974128337e06e497d95a1dc09e86fd2a01a405f455f", 16397952536},
			"clip_vision_h.safetensors":                    {"64a7ef761bfccbadbaa3da77366aac4185a6c58fa5de5f589b42a65bcc21f161", 1264219396},
		},
		"hunyuanvideo15-720p-t2v": {
			"hunyuanvideo1.5_720p_t2v_fp16.safetensors": {"4f05e2a31b51b7ddee3ef336d234e0932001cbdc85887da15451069fda3e1d05", 16653368128},
		},
		"hunyuanvideo15-720p-i2v": {
			"hunyuanvideo1.5_720p_i2v_cfg_distilled_fp8_scaled.safetensors": {"ee7680ea04685d72b1030cc00178954e11062ad558f66acce3ffe26454dc29c2", 8330399746},
			"sigclip_vision_patch14_384.safetensors":                        {"1fee501deabac72f0ed17610307d7131e3e9d1e838d0363aa3c2b97a6e03fb33", 856505640},
		},
	} {
		m := model(t, id)
		assert.Equal(t, localcfg.MediaVideo, m.Kind, id)
		assert.Equal(t, catalog.RatingGeneral, m.ContentRating, id)
		assert.False(t, m.Traits.Uncensored, id)
		assert.Equal(t, "2026-11-03", m.License.CheckedAt, id)
		for name, want := range pins {
			f := fileOf(t, m, name)
			assert.Equal(t, want.sha, f.SHA256, id+" "+name)
			assert.Equal(t, want.size, f.Size, id+" "+name)
			assert.Empty(t, f.SourceNote, id+" "+name+": not a mirror")
			assert.True(t, strings.HasPrefix(f.URL, "https://huggingface.co/Comfy-Org/"), id+" "+name)
		}
		assert.NotEmpty(t, m.Workflows, id)
		assert.False(t, m.RequiresHFToken, id+": not gated, so no token")
	}
}

// The Apache-2.0 ones are commercial-free and need no consent; the Hunyuan ones are the Tencent license, conditional for commerce, not
// usable in the EU, the UK and Korea, and ask for the consent at the install.
func TestStandardVideoModelsLicenses(t *testing.T) {
	for _, id := range []string{"wan22-t2v-14b", "wan21-t2v-1-3b", "wan21-flf2v-14b-720p"} {
		m := model(t, id)
		assert.Equal(t, "apache-2.0", m.License.ID, id)
		assert.Equal(t, modellicense.Yes, m.License.Commercial, id)
		assert.Equal(t, catalog.TierStandard, m.Tier, id)
		assert.False(t, m.ConsentRequired, id)
		assert.Contains(t, m.Description, "Apache-2.0", id)
	}
	for _, id := range []string{"hunyuanvideo15-720p-t2v", "hunyuanvideo15-720p-i2v"} {
		m, base := model(t, id), model(t, "hunyuanvideo15-480p")
		assert.Equal(t, "tencent-hunyuan-community", m.License.ID, id)
		assert.Equal(t, modellicense.Conditional, m.License.Commercial, id)
		assert.Equal(t, base.License.ExcludedRegions, m.License.ExcludedRegions, id)
		assert.NotEmpty(t, m.License.ExcludedRegions, id)
		assert.True(t, m.ConsentRequired, id)
		assert.Contains(t, m.Description, "EU・英国・韓国", id)
	}
}

// The shared files are the same file (destination, hash and size) as in the models that already ship them, so that the install fetches
// each once and the delete keeps it while another model uses it. The Wan 2.2 T2V shares the text encoder and the Wan 2.1 VAE with the I2V
// 14B; the 1.3B and the FLF2V share them too; the Hunyuan 720p ones share the encoders and the VAE with the 480p.
func TestStandardVideoModelsShareTheirEncodersAndVAEs(t *testing.T) {
	shared := map[string][]string{
		"wan22-i2v-14b":       {"wan22-t2v-14b", "wan21-t2v-1-3b", "wan21-flf2v-14b-720p"},
		"hunyuanvideo15-480p": {"hunyuanvideo15-720p-t2v", "hunyuanvideo15-720p-i2v"},
	}
	for baseID, users := range shared {
		base := model(t, baseID)
		for _, id := range users {
			m := model(t, id)
			n := 0
			for _, f := range base.Files {
				if f.Folder == "diffusion_models" {
					continue
				}
				if i := fileIndex(m, f.Name); i >= 0 {
					assert.Equal(t, f, m.Files[i], id+" "+f.Name)
					n++
				}
			}
			assert.Positive(t, n, id+" shares encoders / VAE with "+baseID)
		}
	}
	// the Wan 2.1 VAE, not the Wan 2.2 5B VAE (wan2.2_vae), is the one the 14B and the 1.3B use
	for _, id := range []string{"wan22-t2v-14b", "wan21-t2v-1-3b", "wan21-flf2v-14b-720p"} {
		fileOf(t, model(t, id), "wan_2.1_vae.safetensors")
		fileOf(t, model(t, id), "umt5_xxl_fp8_e4m3fn_scaled.safetensors")
	}
}

func fileIndex(m catalog.Model, name string) int {
	for i, f := range m.Files {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// The traits agree with the workflows: the T2V ones take text only, the FLF2V one takes two frames, and the I2V one takes a first frame.
// The parameter counts and the tiers (small 1.3B, medium 8.3B / 14B) are what the descriptions say.
func TestStandardVideoModelsTraits(t *testing.T) {
	tests := []struct {
		id     string
		params float64
		label  string
		speed  traits.Level
		first  bool
		last   bool
		tier   string
	}{
		{"wan21-t2v-1-3b", 1.3, "1.3B", traits.Fast, false, false, "小ティア"},
		{"wan22-t2v-14b", 14, "14B", traits.Slow, false, false, "中ティア"},
		{"wan21-flf2v-14b-720p", 14, "14B", traits.Slow, true, true, "中ティア"},
		{"hunyuanvideo15-720p-t2v", 8.3, "8.3B", traits.Slow, false, false, "中ティア"},
		{"hunyuanvideo15-720p-i2v", 8.3, "8.3B", traits.Slow, true, false, "中ティア"},
	}
	for _, tt := range tests {
		m := model(t, tt.id)
		assert.Equal(t, tt.params, m.Traits.ParamsB, tt.id)
		assert.Equal(t, tt.label, m.Traits.ParamsLabel, tt.id)
		assert.Equal(t, tt.speed, m.Traits.Speed, tt.id)
		assert.Contains(t, m.Description, tt.tier, tt.id)
		assert.Contains(t, m.Description, "音声なし", tt.id)
		require.Len(t, m.Workflows, 1, tt.id)
		caps, ok := mustTemplate(t, m.Workflows[0].Template).Model.VideoCaps()
		require.True(t, ok, tt.id)
		assert.Equal(t, tt.first, caps.FirstFrame, tt.id)
		assert.Equal(t, tt.last, caps.LastFrame, tt.id)
	}
}

// None of the new workflows needs a custom node (the bundled ComfyUI starts with --disable-all-custom-nodes).
func TestStandardVideoModelsNeedNoCustomNode(t *testing.T) {
	for _, id := range []string{"wan22-t2v-14b", "wan21-t2v-1-3b", "wan21-flf2v-14b-720p", "hunyuanvideo15-720p-t2v", "hunyuanvideo15-720p-i2v"} {
		for _, wf := range model(t, id).Workflows {
			g, err := localcfgParse(t, wf.Template)
			require.NoError(t, err)
			for nid, n := range g {
				assert.NotContains(t, n.ClassType, "KJ", wf.Template+" "+nid)
				assert.NotContains(t, n.ClassType, "WanVideo", wf.Template+" "+nid)
				assert.NotContains(t, n.ClassType, "VHS_", wf.Template+" "+nid)
			}
		}
	}
}

func localcfgParse(t *testing.T, template string) (localcfg.Graph, error) {
	t.Helper()
	return localcfg.ParseGraph(mustTemplate(t, template).Model.Workflow.Graph)
}
