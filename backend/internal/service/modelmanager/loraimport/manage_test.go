package loraimport_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile/lorafiletest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
)

// Update replaces what the user confirmed and leaves the file alone; Delete removes the file and its record, and never touches a file nobody recorded.
func TestUpdateAndDelete(t *testing.T) {
	r := newRig(t, nil)
	r.put("hand.safetensors", lorafiletest.LoRA(nil))
	st := r.mustStage("a.safetensors", lorafiletest.LoRA(nil))
	m, err := r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token})
	require.NoError(t, err)

	s := 0.5
	u, err := r.svc.Update(context.Background(), m.Path, loraimport.Meta{BaseModels: []string{"flux"}, Strength: &s, TriggerWords: "zz"})
	require.NoError(t, err)
	assert.Equal(t, []string{"flux"}, u.BaseModels)
	assert.Equal(t, []string{"zz"}, u.TriggerWords)
	assert.Equal(t, m.SHA256, u.SHA256)
	byName, ok := r.svc.ByName("a.safetensors")
	require.True(t, ok)
	assert.Equal(t, []string{"zz"}, byName.TriggerWords)
	_, ok = r.svc.ByName("hand.safetensors")
	assert.False(t, ok, "a file nobody recorded has no record")

	_, err = r.svc.Update(context.Background(), "models/loras/hand.safetensors", loraimport.Meta{})
	assert.ErrorIs(t, err, loraimport.ErrNotFound)
	assert.ErrorIs(t, r.svc.Delete(context.Background(), "models/loras/hand.safetensors"), loraimport.ErrNotFound)
	assert.Contains(t, r.files(), "hand.safetensors")

	require.NoError(t, r.svc.Delete(context.Background(), m.Path))
	assert.NotContains(t, r.files(), "a.safetensors")
	assert.Empty(t, r.svc.List())
}

// The one-call import (the MCP upload) refuses a taken name and keeps nothing; with adopt it fills the base model and trigger words from the file, which
// the caller did not give, and never the strength or the license.
func TestImport_OneCall(t *testing.T) {
	r := newRig(t, nil)
	body := lorafiletest.LoRA(map[string]string{"ss_base_model_version": "sdxl_base_v1-0", "modelspec.trigger_phrase": "tw", "modelspec.license": "MIT"})
	m, err := r.svc.Import(context.Background(), "one.safetensors", int64(len(body)), bytes.NewReader(body), loraimport.Input{}, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"sdxl"}, m.BaseModels)
	assert.Equal(t, []string{"tw"}, m.TriggerWords)
	assert.Nil(t, m.Strength)
	assert.Empty(t, m.LicenseName)

	_, err = r.svc.Import(context.Background(), "one.safetensors", int64(len(body)), bytes.NewReader(body), loraimport.Input{}, true)
	assert.Contains(t, refusal(t, err), "同じ内容のファイル")
	assert.Empty(t, r.leftovers())
	assert.Len(t, r.files(), 1)
}
