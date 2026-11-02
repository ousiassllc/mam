package lightmodelstest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// FLUX.2 klein 4B (Issue #931): the Apache-2.0 4B in ComfyUI's fp8 distilled form. The diffusion model comes from BFL's own
// FP8 repository (no mirror), the text encoder and the VAE from Comfy-Org's klein repackaging; each is pinned to a commit with
// the LFS SHA-256 and size Hugging Face returns (X-Linked-Etag, X-Linked-Size) as read on 2026-11-02.
func TestFlux2Klein4BIsPinnedAndCommercialUseIsAllowed(t *testing.T) {
	m, ok := catalog.Default().Model("flux2-klein-4b")
	require.True(t, ok)
	assert.Equal(t, catalog.TierStandard, m.Tier, "Apache-2.0 and not adult: no consent")
	assert.Equal(t, catalog.RatingGeneral, m.ContentRating)
	assert.False(t, m.ConsentRequired)
	assert.False(t, m.RequiresHFToken, "neither repository is gated")
	assert.Equal(t, modellicense.Yes, m.License.Commercial)
	assert.Equal(t, "apache-2.0", m.License.ID)
	assert.Equal(t, "2026-11-02", m.License.CheckedAt)
	assert.Len(t, m.License.Components, 3)
	assert.Contains(t, m.License.URL, "FLUX.2-klein-4b-fp8", "the license text of the 4B repository, not the 9B's")

	want := map[string]struct {
		path, url, sha256 string
		size              int64
	}{
		"flux-2-klein-4b-fp8.safetensors": {
			"models/diffusion_models/flux-2-klein-4b-fp8.safetensors",
			"https://huggingface.co/black-forest-labs/FLUX.2-klein-4b-fp8/resolve/5b4408e59397a4a37ccb46afe426d8ed86379441/flux-2-klein-4b-fp8.safetensors",
			"97ed34fe0567e436200f2faee3939b88f2b5d99f8af2a4dc16532c4245c0ccb6", 4070624520,
		},
		"qwen_3_4b.safetensors": {
			"models/text_encoders/qwen_3_4b.safetensors",
			"https://huggingface.co/Comfy-Org/Flux-2-Klein-4B/resolve/5b3abca5c0dc1ecd4eaf3340868d09063fc1c965/split_files/text_encoders/qwen_3_4b.safetensors",
			"6c671498573ac2f7a5501502ccce8d2b08ea6ca2f661c458e708f36b36edfc5a", 8044982048,
		},
		"flux2-vae.safetensors": {
			"models/vae/flux2-vae.safetensors",
			"https://huggingface.co/Comfy-Org/Flux-2-Klein-4B/resolve/5b3abca5c0dc1ecd4eaf3340868d09063fc1c965/split_files/vae/flux2-vae.safetensors",
			"868fe7b343cc8f3a19dbcfcafbc3d5f888802be3f89bd81b65b3621a066ce8f3", 336211292,
		},
	}
	require.Len(t, m.Files, len(want))
	for _, f := range m.Files {
		w, ok := want[f.Name]
		require.True(t, ok, f.Name)
		assert.Equal(t, w.path, f.Path(), f.Name)
		assert.Equal(t, w.url, f.URL, f.Name)
		assert.Equal(t, w.sha256, f.SHA256, f.Name)
		assert.Equal(t, w.size, f.Size, f.Name)
		assert.Empty(t, f.SourceNote, "no third-party mirror: "+f.Name)
	}

	assert.Equal(t, 12, m.VRAM.MinGB)
	assert.True(t, m.VRAM.Estimate, "not measured on a 16 GB card by mam")
}

// One workflow does both jobs: the text-to-image one and the editing one with up to four reference images. No 9B or dev file
// is in the model.
func TestFlux2KleinHasOneWorkflowForGenerationAndEditing(t *testing.T) {
	m, _ := catalog.Default().Model("flux2-klein-4b")
	require.Equal(t, []catalog.Workflow{{Name: "flux2-klein-4b", Template: "txt2img-flux2-klein"}}, m.Workflows)
	tpl, ok := localcfg.TemplateByID("txt2img-flux2-klein")
	require.True(t, ok)
	caps := tpl.Model.ReferenceCaps()
	assert.Equal(t, localcfg.ModeEdit, caps.Mode)
	assert.Equal(t, 4, caps.Max)

	for _, f := range m.Files {
		assert.NotContains(t, f.Name, "9b", "the 9B is non-commercial")
		assert.NotContains(t, f.Name, "dev", "FLUX.2 dev is non-commercial")
	}
	assert.Contains(t, m.Description, "検閲", "the safety-tuned model says so on its card")
}
