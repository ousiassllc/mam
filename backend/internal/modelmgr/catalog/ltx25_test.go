package catalog_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// ltx25Commit is the commit of Lightricks/LTX-2.5 the files are pinned to.
const ltx25Commit = "49b093a39c19169a8a81721dcc541e07e945232f"

// LTX-2.5 (Issue #877) is a gated repository: its five files are pinned to one commit of the Lightricks repository itself (no
// mirror), with the LFS SHA-256 and size read from Hugging Face (the oid of paths-info, which is the X-Linked-Etag of the file).
func TestLTX25IsPinnedToTheGatedRepositoryAtOneCommit(t *testing.T) {
	m, ok := catalog.Default().Model("ltx-2.5-22b-distilled")
	require.True(t, ok)
	assert.True(t, m.RequiresHFToken)
	assert.True(t, m.ConsentRequired, "the license has a revenue condition: the consent is needed")
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.Equal(t, catalog.RatingGeneral, m.ContentRating)
	assert.True(t, m.License.CommercialUse)
	assert.Contains(t, m.License.CommercialCondition, "1,000 万米ドル")
	assert.True(t, m.VRAM.Estimate, "no published minimum: mam's own estimate")

	type pin struct {
		path   string
		sha256 string
		size   int64
	}
	want := map[string]pin{
		"diffusion_models/ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors": {"diffusion_models/ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors", "c4279eeff115cbeaca494bd2183e7d768c38fe85a184dc6afbb7159157c44334", 21504034224},
		"text_encoders/gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors":         {"text_encoders/gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors", "6ce688a0aa98a5fa36a9f1e6c3f42152a498cc2b53ee8c15674c64244f91487f", 15372969374},
		"vae/ltx-2.5-video-vae-bf16.safetensors":                                            {"vae/ltx-2.5-video-vae-bf16.safetensors", "847e14ca7f3355debca0cea4eaa24ac0fbcdf0061da054ac89ca638a869ddba3", 1472223346},
		"vae/ltx-2.5-audio-vae-bf16.safetensors":                                            {"vae/ltx-2.5-audio-vae-bf16.safetensors", "c52733d37f6a7fb7949c3dc0fb468c6cb2169e4d836983a73babb9f0d54837a5", 364866540},
		"latent_upscale_models/ltx-2.5-latent-spatial-upscaler-x2-bf16-1.0.safetensors":     {"latent_upscale_models/ltx-2.5-latent-spatial-upscaler-x2-bf16-1.0.safetensors", "eb5a71fe4068ee87ccdb1c3aa635e547ca76bd2d30ae20ae889f2c325c0677e8", 995778752},
	}
	require.Len(t, m.Files, len(want))
	for _, f := range m.Files {
		p, ok := want[f.Folder+"/"+f.Name]
		require.True(t, ok, f.Folder+"/"+f.Name)
		assert.Equal(t, "https://huggingface.co/Lightricks/LTX-2.5/resolve/"+ltx25Commit+"/"+p.path, f.URL)
		assert.Equal(t, p.sha256, f.SHA256, f.Name)
		assert.Equal(t, p.size, f.Size, f.Name)
		assert.Empty(t, f.SourceNote, f.Name+": no third-party mirror")
	}
	assert.Equal(t, int64(39709872236), m.TotalSize())
}

// The old LTX-Video 0.9.5 stays next to LTX-2.5 (a separate model and template, nothing a user assigned is removed).
func TestTheOldLTXVideoStaysNextToLTX25(t *testing.T) {
	c := catalog.Default()
	old, ok := c.Model("ltx-video-2b")
	require.True(t, ok)
	assert.False(t, old.RequiresHFToken)
	assert.Equal(t, "txt2video-ltx-v095", old.Workflows[0].Template)
	m, _ := c.Model("ltx-2.5-22b-distilled")
	var templates []string
	for _, w := range m.Workflows {
		templates = append(templates, w.Template)
		assert.True(t, strings.HasPrefix(w.Name, "ltx-2-5-"), w.Name)
	}
	assert.Equal(t, []string{"txt2video-ltx-2-5", "img2video-ltx-2-5"}, templates)
}
