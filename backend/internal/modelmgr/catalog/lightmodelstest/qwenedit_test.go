package lightmodelstest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/vramfit"
)

// Qwen-Image-Edit 2511 (Issue #971): the official Apache-2.0 editing model in Comfy-Org's fp8mixed repackaging. The diffusion model
// is pinned to the commit of Comfy-Org/Qwen-Image-Edit with the LFS SHA-256 and size Hugging Face returned on 2026-11-03; the text
// encoder and the VAE are the very files of qwen-image (the same commit of Comfy-Org/Qwen-Image), so an installed qwen-image
// leaves only the diffusion model to fetch.
func TestQwenImageEdit2511IsPinnedAndCommercialUseIsAllowed(t *testing.T) {
	c := catalog.Default()
	m, ok := c.Model("qwen-image-edit-2511")
	require.True(t, ok)
	assert.Equal(t, catalog.TierStandard, m.Tier, "Apache-2.0 and not adult: no consent")
	assert.Equal(t, catalog.RatingGeneral, m.ContentRating)
	assert.False(t, m.ConsentRequired)
	assert.False(t, m.RequiresHFToken, "the repository is not gated")
	assert.Equal(t, modellicense.Yes, m.License.Commercial)
	assert.Equal(t, "apache-2.0", m.License.ID)
	assert.Equal(t, "2026-11-03", m.License.CheckedAt)

	require.Len(t, m.Files, 5)
	edit := m.Files[0]
	assert.Equal(t, "models/diffusion_models/qwen_image_edit_2511_fp8mixed.safetensors", edit.Path())
	assert.Equal(t, "https://huggingface.co/Comfy-Org/Qwen-Image-Edit/resolve/f68ace85e60b4a02a323e394253731947657b7d2/split_files/diffusion_models/qwen_image_edit_2511_fp8mixed.safetensors", edit.URL)
	assert.Equal(t, "c9fdc158e46d3b61ef75f21ae866ca2fe808bf4a53643120d1c1e87c19280a4e", edit.SHA256)
	assert.Equal(t, int64(20533762817), edit.Size)
	assert.Empty(t, edit.SourceNote, "no third-party mirror")

	// The text encoder and the VAE are qwen-image's files: the same destination, URL, SHA-256 and size, so that the install shares them.
	qwen, _ := c.Model("qwen-image")
	assert.Equal(t, qwen.Files[1], m.Files[1])
	assert.Equal(t, qwen.Files[2], m.Files[2])
}

// The two Lightning LoRAs of the speed modes (Issue #991) are pinned to the commit of lightx2v/Qwen-Image-Edit-2511-Lightning with the
// LFS SHA-256 and size Hugging Face returned on 2026-11-03 (the bf16 ones: the card is Apache-2.0 and the repository is not gated), and
// they are the files the LoraLoaderModelOnly nodes of the template load.
func TestQwenImageEdit2511HasTheLightningLoRAsPinned(t *testing.T) {
	m, _ := catalog.Default().Model("qwen-image-edit-2511")
	const commit = "107d238d68b8387e32843d0b4a2e24ceb0d3ad86"
	for i, want := range []struct {
		name, sha string
	}{
		{"Qwen-Image-Edit-2511-Lightning-4steps-V1.0-bf16.safetensors", "22226e8d05d354bb356627d428809f5afd7819399b077238a2b70a82883a904f"},
		{"Qwen-Image-Edit-2511-Lightning-8steps-V1.0-bf16.safetensors", "a9e81a58a78f260f67b337a6f615e8fa4cd3bc79847c77b7d61a581b789b1ba8"},
	} {
		f := m.Files[3+i]
		assert.Equal(t, "models/loras/"+want.name, f.Path())
		assert.Equal(t, "https://huggingface.co/lightx2v/Qwen-Image-Edit-2511-Lightning/resolve/"+commit+"/"+want.name, f.URL)
		assert.Equal(t, want.sha, f.SHA256)
		assert.Equal(t, int64(849608296), f.Size)
		assert.Empty(t, f.SourceNote, "no third-party mirror")
	}
	tpl, ok := localcfg.TemplateByID("txt2img-qwen-image-edit-2511")
	require.True(t, ok)
	g, err := tpl.Model.Workflow.GraphWith(nil)
	require.NoError(t, err)
	assert.JSONEq(t, `"`+m.Files[3].Name+`"`, string(g["90"].Inputs["lora_name"]), "the 4 step LoRA")
	assert.JSONEq(t, `"`+m.Files[4].Name+`"`, string(g["91"].Inputs["lora_name"]), "the 8 step LoRA")
}

// The workflow is the editing one with up to three references (the EditPlus encoder's image1..image3), and the license of the
// noctq model (Qwen Research, non-commercial) does not leak into it.
func TestQwenImageEdit2511HasTheThreeReferenceEditingWorkflow(t *testing.T) {
	m, _ := catalog.Default().Model("qwen-image-edit-2511")
	require.Equal(t, []catalog.Workflow{{Name: "qwen-image-edit-2511", Template: "txt2img-qwen-image-edit-2511"}}, m.Workflows)
	tpl, ok := localcfg.TemplateByID("txt2img-qwen-image-edit-2511")
	require.True(t, ok)
	caps := tpl.Model.ReferenceCaps()
	assert.Equal(t, localcfg.ModeEdit, caps.Mode)
	assert.Equal(t, 3, caps.Max)
	assert.Equal(t, []string{localcfg.RoleWhole, localcfg.RoleFace, localcfg.RoleStyle, localcfg.RoleEdit, localcfg.RolePose}, caps.Roles)
	assert.True(t, tpl.Model.ImageInputs(localcfg.KindComfyUI))
	assert.True(t, tpl.Model.SupportsLoRA())
}

// The VRAM guideline says plainly that 16 GB is an estimate that needs the offload: Hugging Face's fit check calls this model "over"
// for 16 GB, and mam has not measured it on a 16 GB card. A 16 GB GPU is "limited" (runs, with the limits shown), not "enough", and
// a smaller one is "short".
func TestQwenImageEdit2511VRAMGuidelineIsHonestAbout16GB(t *testing.T) {
	m, _ := catalog.Default().Model("qwen-image-edit-2511")
	assert.True(t, m.VRAM.Estimate)
	assert.Contains(t, m.VRAM.Source, "実機未検証")
	assert.Contains(t, m.VRAM.Limits, "オフロード")
	const gb = uint64(1) << 30
	assert.Equal(t, vramfit.Short, vramfit.For(m.VRAM, 12*gb))
	assert.Equal(t, vramfit.Limited, vramfit.For(m.VRAM, 16*gb))
	assert.Equal(t, vramfit.Enough, vramfit.For(m.VRAM, 24*gb))
}
