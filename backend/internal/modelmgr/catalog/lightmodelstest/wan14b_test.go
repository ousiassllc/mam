package lightmodelstest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/adult"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

func model(t *testing.T, id string) catalog.Model {
	t.Helper()
	m, ok := catalog.Default().Model(id)
	require.True(t, ok, id)
	return m
}

func fileOf(t *testing.T, m catalog.Model, name string) catalog.File {
	t.Helper()
	i := slices.IndexFunc(m.Files, func(f catalog.File) bool { return f.Name == name })
	require.GreaterOrEqual(t, i, 0, m.ID+" has "+name)
	return m.Files[i]
}

// The LightX2V 4-step LoRA (Issue #930) rides on the official 14B: the same four files plus the high and the low noise LoRA, Apache-2.0
// by the model card, a general model (not adult, no consent). The pins are what the Hugging Face API returned on 2026-11-02.
func TestLightX2VIsTheOfficial14BPlusTheFourStepLoRA(t *testing.T) {
	m, base := model(t, "wan22-i2v-14b-lightx2v"), model(t, "wan22-i2v-14b")
	assert.Equal(t, catalog.TierStandard, m.Tier)
	assert.Equal(t, catalog.RatingGeneral, m.ContentRating)
	assert.False(t, m.ConsentRequired)
	assert.Equal(t, modellicense.Yes, m.License.Commercial)
	assert.Equal(t, "apache-2.0", m.License.ID)
	assert.Equal(t, "2026-11-02", m.License.CheckedAt)
	require.Len(t, m.License.Components, 2, "the base and the LoRA are both listed")
	for _, f := range base.Files {
		assert.Contains(t, m.Files, f, "the base files are shared, so the base stays installed when only the LoRA model is deleted")
	}
	high := fileOf(t, m, "wan2.2_i2v_A14b_high_noise_lora_rank64_lightx2v_4step_1022.safetensors")
	low := fileOf(t, m, "wan2.2_i2v_A14b_low_noise_lora_rank64_lightx2v_4step_1022.safetensors")
	assert.Equal(t, "887c3bdeb74e83859c920438e16ca31f39ab18ce189abc5f0e36f8348c5bbb19", high.SHA256)
	assert.Equal(t, int64(634645944), high.Size)
	assert.Equal(t, "8833bd4fd7c8eabebf0bc8ee5cfaf47f4f310ce116928a02c1adf8941dd4b0f1", low.SHA256)
	assert.Equal(t, int64(739472104), low.Size)
	for _, f := range []catalog.File{high, low} {
		assert.Equal(t, "loras", f.Folder)
		assert.True(t, strings.HasPrefix(f.URL, "https://huggingface.co/lightx2v/Wan2.2-Distill-Loras/resolve/"), "pinned to a commit of the author's repository")
		assert.Empty(t, f.SourceNote, "not a mirror")
	}
	assert.Equal(t, []catalog.Workflow{{Name: "wan22-i2v-14b-lightx2v", Template: "img2video-wan22-14b-lightx2v"}}, m.Workflows)
	assert.Contains(t, base.Description, "wan22-i2v-14b-lightx2v", "the base card points to the fast version")
	assert.Contains(t, m.Description, "4 ステップ")
}

// The uncensored 4-step pair (rzgar, Issue #930) is an adult model: advanced, consent, the adult gate judges its workflow and its
// LoRA files adult, and the base it shares with the general models stays a general file. Its Apache-2.0 is the model card's word only
// (no LICENSE text, no word on the training data), so the answer is 不明 with the notice, and the parts show why.
func TestTheUncensoredLightX2VIsAdultAndSaysWhatItsLicenseRestsOn(t *testing.T) {
	m := model(t, "wan22-i2v-14b-lightx2v-uncensored")
	assert.Equal(t, catalog.RatingAdult, m.ContentRating)
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.True(t, m.ConsentRequired)
	assert.Equal(t, modellicense.Unknown, m.License.Commercial, "declared on the card only: not known")
	assert.Contains(t, m.License.Notice, "宣言", "the license is the card's declaration, with no LICENSE text")
	assert.Contains(t, m.License.Notice, "商用には使わないでください")
	require.Len(t, m.License.Components, 2)
	assert.Equal(t, modellicense.Yes, m.License.Components[0].Commercial, "the base")
	assert.Equal(t, modellicense.Unknown, m.License.Components[1].Commercial, "the LoRA")
	for name, want := range map[string]struct {
		sha  string
		size int64
	}{
		"Wan2.2_LightX2V_high_n54vv.safetensors": {"13be3b91290212ada7f9097d7aa1c1354493004952a65794acafa0e9f66122ac", 1245752600},
		"Wan2.2_LightX2V_low_n54vv.safetensors":  {"85fb420fdcd219c08fed2432f932179103546ac2c55a463feffecdce1c6a6f5d", 1245752600},
	} {
		f := fileOf(t, m, name)
		assert.Equal(t, want.sha, f.SHA256, name)
		assert.Equal(t, want.size, f.Size, name)
		assert.Contains(t, f.URL, "/rzgar/Wan2.2_LightX2V_4Step_Uncensored/resolve/", name)
		assert.Contains(t, f.SourceNote, "2026-11-02", name)
	}

	cat := catalog.Default()
	files := adult.FilesOf(cat, store.State{})
	assert.True(t, files["wan2.2_lightx2v_high_n54vv.safetensors"])
	assert.False(t, files["wan2.2_i2v_high_noise_14b_fp8_scaled.safetensors"], "the base is shared with the general 14B")
	assert.False(t, files["wan2.2_i2v_a14b_high_noise_lora_rank64_lightx2v_4step_1022.safetensors"], "the general LightX2V is not adult")
	tpl, _ := localcfg.TemplateByID("img2video-wan22-14b-lightx2v-uncensored")
	assert.True(t, files.Reads(tpl.Model.Workflow.Graph))
	general, _ := localcfg.TemplateByID("img2video-wan22-14b-lightx2v")
	assert.False(t, files.Reads(general.Model.Workflow.Graph), "the general LightX2V workflow is not hidden behind the age gate")
	workflows := adult.Workflows(cat, store.State{})
	assert.True(t, workflows["wan22-i2v-14b-lightx2v-uncensored"])
	assert.False(t, workflows["wan22-i2v-14b-lightx2v"])
}

// Remix T2V v2.0 (Issue #930) joins the Remix I2V v3.0 that was already there: adult, the license "unknown" (the Hugging Face card says
// license: other with no text), the files pinned to the mirror and checked against Civitai's hashes, the text encoder and the VAE shared
// with the I2V, a 16 GB figure marked as an estimate, and a card that says where it does and does not run.
func TestRemixHasBothTextToVideoAndImageToVideo(t *testing.T) {
	t2v, i2v := model(t, "wan22-remix-nsfw-t2v"), model(t, "wan22-remix-nsfw-i2v")
	for _, m := range []catalog.Model{t2v, i2v} {
		assert.Equal(t, catalog.RatingAdult, m.ContentRating, m.ID)
		assert.True(t, m.ConsentRequired, m.ID)
		assert.Equal(t, modellicense.Unknown, m.License.Commercial, m.ID)
		assert.NotEmpty(t, m.License.Notice, m.ID)
		assert.Equal(t, 16, m.VRAM.MinGB, m.ID)
		assert.True(t, m.VRAM.Estimate, m.ID)
		assert.Contains(t, m.Description, "macOS", m.ID+": says where it does not run")
		assert.Contains(t, m.Description, "Windows", m.ID)
	}
	high := fileOf(t, t2v, "Wan2.2_Remix_NSFW_t2v_14b_high_lighting_v2.0.safetensors")
	low := fileOf(t, t2v, "Wan2.2_Remix_NSFW_t2v_14b_low_lighting_v2.0.safetensors")
	assert.Equal(t, "b1c4725cfe80ea3a01f9af518f7a2f343320e1173482b8d8fb08858ddc72c69a", high.SHA256, "Civitai's hash of T2V High v2.0 too")
	assert.Equal(t, "d9457044066af43e24c1d215842e1540ecc29b724ac6a7b96f0ddaee0d56950e", low.SHA256, "Civitai's hash of T2V Low v2.0 too")
	for _, f := range []catalog.File{high, low} {
		assert.Equal(t, int64(14289633736), f.Size)
		assert.Contains(t, f.SourceNote, "model 2003153")
		assert.Contains(t, f.SourceNote, "2026-11-02")
	}
	for _, shared := range []string{"umt5_xxl_fp8_e4m3fn_scaled.safetensors", "wan_2.1_vae.safetensors"} {
		assert.Equal(t, fileOf(t, i2v, shared), fileOf(t, t2v, shared), shared)
	}
	assert.Equal(t, []catalog.Workflow{{Name: "wan22-remix-nsfw-t2v", Template: "txt2video-wan22-14b-remix"}}, t2v.Workflows)
	tpl, _ := localcfg.TemplateByID("txt2video-wan22-14b-remix")
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.False(t, caps.FirstFrame, "text to video")
	icaps, _ := mustTemplate(t, "img2video-wan22-14b-remix").Model.VideoCaps()
	assert.True(t, icaps.FirstFrame, "image to video")
	assert.Contains(t, i2v.Description, "wan22-remix-nsfw-t2v")
	assert.Contains(t, t2v.Description, "wan22-remix-nsfw-i2v")
}

func mustTemplate(t *testing.T, id string) localcfg.Template {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return tpl
}

// None of the Wan 2.2 14B workflows needs a custom node: the bundled ComfyUI starts with --disable-all-custom-nodes, so mam builds the Remix
// and LightX2V workflows from core nodes (the authors' KJNodes / WanVideoWrapper ones are not used).
func TestTheWan14BFamilyNeedsNoCustomNode(t *testing.T) {
	for _, id := range []string{"wan22-i2v-14b", "wan22-i2v-14b-lightx2v", "wan22-i2v-14b-lightx2v-uncensored", "wan22-remix-nsfw-i2v", "wan22-remix-nsfw-t2v", "wan22-i2v-14b-nsfw-lora"} {
		for _, wf := range model(t, id).Workflows {
			g, err := localcfg.ParseGraph(mustTemplate(t, wf.Template).Model.Workflow.Graph)
			require.NoError(t, err)
			for nid, n := range g {
				assert.NotContains(t, n.ClassType, "KJ", wf.Template+" "+nid)
				assert.NotContains(t, n.ClassType, "WanVideo", wf.Template+" "+nid)
				assert.NotContains(t, n.ClassType, "VHS_", wf.Template+" "+nid)
			}
		}
	}
}
