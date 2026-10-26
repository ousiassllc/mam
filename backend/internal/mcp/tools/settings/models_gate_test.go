package settings_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
)

// adultLocal is a local image model that the age gate of this test treats as an adult model while it is closed.
const adultLocal = "openai-compat:oa/x"

func imageModelNames(m settings.ModelsSection) []string {
	var out []string
	for _, o := range m.ImageModels {
		out = append(out, o.Model)
	}
	return out
}

// An agent that lists the assignment sees which adult models it still names and cannot use (gated_models) and does not find them
// among the models it may choose; setting a route that names one, new or kept, is refused with the reason.
func TestSetModelAssignment_AnAdultModelIsNotOfferedAndNotAcceptedWhileTheAgeConfirmationIsOff(t *testing.T) {
	t.Cleanup(func() { modelreg.Set(nil, nil) })
	modelreg.Set([]string{adultLocal}, nil)
	s, f := newFiles(t, false)
	require.NoError(t, os.MkdirAll(f.models[:len(f.models)-len("models.json")], 0o750))
	require.NoError(t, os.WriteFile(f.models, []byte(`{
	  "endpoints": {"oa": {"kind": "openai-compat", "base_url": "http://192.168.1.9:8000"}},
	  "local_models": {"oa/x": {"media": "image", "model": "x"}},
	  "images": {"preview": {"primary": {"model": "`+adultLocal+`"}, "fallback": {"model": "nano-banana"}}}
	}`), 0o600))
	closed := false
	s.Models.SetHidden(func(id string) bool { return closed && id == adultLocal })
	env := newEnv(t, s)

	var open settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &open)
	assert.Contains(t, imageModelNames(open.Models), adultLocal)
	assert.Empty(t, open.Models.GatedModels)

	closed = true
	var off settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &off)
	assert.NotContains(t, imageModelNames(off.Models), adultLocal, "not a choice")
	assert.Equal(t, []string{adultLocal}, off.Models.GatedModels)
	assert.Contains(t, off.Models.Images[2].Primary.Model, adultLocal, "the assignment is listed as it is saved")

	route := func(purpose, primary string) map[string]any {
		return map[string]any{"images": []map[string]any{{
			"purpose": purpose, "primary": map[string]any{"model": primary}, "fallback": map[string]any{"model": "nano-banana"},
		}}}
	}
	assert.Contains(t, env.CallError("set_model_assignment", route("new_reference", adultLocal)), "年齢確認がオフ", "a new choice")
	assert.Contains(t, env.CallError("set_model_assignment", route("edit_reference", "flux-2")), "年齢確認がオフ", "the kept route of another purpose")

	var saved settings.ModelsSection
	env.CallOK("set_model_assignment", route("preview", "nano-banana"), &saved)
	assert.Empty(t, saved.GatedModels, "replacing the route that names it clears it")
}
