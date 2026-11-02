package modelcatalog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

func workflowModel(media localcfg.Media, mode localcfg.ReferenceMode, refs int, lora string) localcfg.SavedWorkflow {
	w := &localcfg.Workflow{ReferenceMode: mode, LoraModelNode: lora}
	for i := range refs {
		w.Assignments = append(w.Assignments, localcfg.Assignment{Param: localcfg.ParamReferenceImage, Node: "1", Input: "image", Index: i})
	}
	return localcfg.SavedWorkflow{Model: localcfg.Model{Media: media, Workflow: w}}
}

func TestReferenceCapsAndLoRAOfAModel(t *testing.T) {
	cfg := modeladapter.Config{Workflows: map[string]localcfg.SavedWorkflow{
		"edit":  workflowModel(localcfg.MediaImage, localcfg.ModeEdit, 3, "7"),
		"i2i":   workflowModel(localcfg.MediaImage, localcfg.ModeImg2Img, 1, ""),
		"plain": workflowModel(localcfg.MediaImage, "", 0, ""),
		"video": workflowModel(localcfg.MediaVideo, "", 1, "7"),
	}}

	edit := modelcatalog.ReferenceCapsOf(cfg, "edit")
	assert.Equal(t, localcfg.ModeEdit, edit.Mode)
	assert.Equal(t, 3, edit.Max)
	assert.Equal(t, []string{"reference", "face", "style", "edit"}, edit.Roles)
	assert.True(t, modelcatalog.SupportsLoRA(cfg, "edit"))

	i2i := modelcatalog.ReferenceCapsOf(cfg, "i2i")
	assert.Equal(t, localcfg.ModeImg2Img, i2i.Mode)
	assert.Equal(t, 1, i2i.Max)
	assert.Equal(t, []string{"reference"}, i2i.Roles)
	assert.False(t, modelcatalog.SupportsLoRA(cfg, "i2i"))

	assert.Zero(t, modelcatalog.ReferenceCapsOf(cfg, "plain"))
	assert.Zero(t, modelcatalog.ReferenceCapsOf(cfg, "video"), "a video model has no image reference caps")
	assert.False(t, modelcatalog.SupportsLoRA(cfg, "video"), "a LoRA is for image models")

	for _, m := range []domain.ModelName{"edit", "i2i", "plain"} {
		assert.Equal(t, modelcatalog.ReferenceCapsOf(cfg, m).Max > 0, cfg.AcceptsImages(m), "AcceptsImages agrees with ReferenceCaps for %s", m)
	}
	assert.Zero(t, modelcatalog.ReferenceCapsOf(cfg, domain.ModelNanoBanana), "a cloud model's references are not described")
	assert.False(t, modelcatalog.SupportsLoRA(cfg, domain.ModelNanoBanana))
	assert.True(t, cfg.AcceptsImages(domain.ModelNanoBanana))
}
