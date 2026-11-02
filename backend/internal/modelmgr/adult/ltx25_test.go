package adult_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/adult"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// The uncensored text encoder of LTX-2.5 (Issue #927) is the one file only the adult model has: a workflow that reads it is judged as the
// adult one, whatever it is named, and the LTX-2.5 files it shares with the standard model are not adult files (the standard model
// stays a general one).
func TestUncensoredLTX25IsJudgedByItsTextEncoder(t *testing.T) {
	cat := catalog.Default()
	files := adult.FilesOf(cat, store.State{})
	assert.True(t, files["gemma4-12b-uncensored-heretic-deepneuralnerd-ltx-2.5-int8-convrot.safetensors"])
	for _, shared := range []string{
		"ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors", "gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors",
		"ltx-2.5-video-vae-bf16.safetensors", "ltx-2.5-audio-vae-bf16.safetensors",
	} {
		assert.False(t, files[shared], shared)
	}

	for id, adultOne := range map[string]bool{
		"txt2video-ltx-2-5-uncensored": true, "img2video-ltx-2-5-uncensored": true,
		"txt2video-ltx-2-5": false, "img2video-ltx-2-5": false,
	} {
		tpl, ok := localcfg.TemplateByID(id)
		require.True(t, ok, id)
		assert.Equal(t, adultOne, files.Reads(tpl.Model.Workflow.Graph), id)
	}

	workflows := adult.Workflows(cat, store.State{})
	assert.True(t, workflows["ltx-2-5-uncensored-t2v"])
	assert.True(t, workflows["ltx-2-5-uncensored-i2v"])
	assert.False(t, workflows["ltx-2-5-t2v"])
}
