package traitstest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// A local video model has video modes (Issue #1014) and no other model does: the field is required where it is the user's way to tell a
// text-to-video model from an image-to-video one, and forbidden where it means nothing.
func TestVideoModesBelongToLocalVideoModelsOnly(t *testing.T) {
	videos := 0
	for _, m := range catalog.Default().Models {
		if m.Kind != localcfg.MediaVideo {
			assert.Empty(t, m.Traits.VideoModes, m.ID+": only a video model has video modes")
			continue
		}
		videos++
		assert.NotEmpty(t, m.Traits.VideoModes, m.ID+": a video model has video modes")
	}
	assert.Positive(t, videos)
}

// The decisions that the workflows alone do not make: a VACE model is not an I2V one although it takes a reference image, the DFR combination
// has no text-only workflow, and the FLF2V model needs both frames (so it is not I2V).
func TestVideoModesOfModelsThatTheWorkflowsDoNotDecide(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string][]traits.VideoMode{
		"wan21-vace-1-3b":           {traits.ModeT2V, traits.ModeVACE, traits.ModeR2V},
		"wan21-flf2v-14b-720p":      {traits.ModeFLF2V},
		"ltx-2.5-22b-distilled-dfr": {traits.ModeI2V},
		"wan22-ti2v-5b":             {traits.ModeT2V, traits.ModeI2V},
		"wan22-rapid-mega-nsfw-t2v": {traits.ModeT2V, traits.ModeI2V, traits.ModeFLF2V},
		"ltx-2.3-22b-distilled":     {traits.ModeT2V, traits.ModeI2V, traits.ModeFLF2V},
		"wan22-i2v-14b":             {traits.ModeI2V},
		"wan22-s2v-14b":             {traits.ModeS2V, traits.ModeR2V},
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		assert.Equal(t, want, m.Traits.VideoModes, id)
	}
}

func TestValidateRefusesBadVideoModes(t *testing.T) {
	for name, tc := range map[string]struct {
		id   string
		edit func(m *catalog.Model)
		want string
	}{
		"a video model without modes":            {"wan22-i2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = nil }, "video_modes is required"},
		"a mode that is none":                    {"wan22-i2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"i2v", "warp"} }, `"warp" must be`},
		"a mode listed twice":                    {"wan22-i2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"i2v", "i2v"} }, `lists "i2v" twice`},
		"modes on an image model":                {"flux1-dev", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v"} }, "belong to a video model"},
		"modes on an upscale model":              {"upscale-nomos-uni-span", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"i2v"} }, "belong to a video model"},
		"t2v for an image-to-video workflow":     {"wan22-i2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v", "i2v"} }, "t2v must be listed if and only if"},
		"no t2v for a text-only workflow":        {"wan22-t2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"vace"} }, "t2v must be listed if and only if"},
		"i2v without a first frame":              {"wan22-t2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v", "i2v"} }, "i2v needs a workflow that takes a first frame"},
		"a first frame that is neither":          {"wan22-ti2v-5b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v"} }, "i2v or flf2v must be listed"},
		"flf2v without a last frame":             {"wan22-i2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"i2v", "flf2v"} }, "flf2v needs a workflow"},
		"a last frame that is not flf2v":         {"ltx-2.5-22b-distilled", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v", "i2v"} }, "flf2v must be listed"},
		"r2v for a workflow without references":  {"wan22-t2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v", "r2v"} }, "r2v must be listed if and only if"},
		"reference images that are not listed":   {"wan21-vace-1-3b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v", "vace"} }, "r2v must be listed if and only if"},
		"the first-frame model of FLF2V as I2V":  {"wan21-flf2v-14b-720p", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"i2v"} }, "flf2v must be listed"},
		"a text-only model that also says flf2v": {"wan21-t2v-1-3b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v", "flf2v"} }, "flf2v needs a workflow"},
	} {
		errs := mutate(t, tc.id, tc.edit)
		if assert.NotEmpty(t, errs, name) {
			joined := make([]string, len(errs))
			for i, e := range errs {
				joined[i] = e.Error()
			}
			assert.Contains(t, strings.Join(joined, "\n"), tc.want, name)
		}
	}
}

// vace is not tied to an input, so it never makes a catalog invalid by itself; s2v is tied to the sound file of a workflow (Issue #1153), as r2v is to the references.
func TestVaceNeedsNoInputOfTheWorkflowsButS2VNeedsASoundWorkflow(t *testing.T) {
	assert.Empty(t, mutate(t, "wan22-t2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v", "vace"} }))
	errs := mutate(t, "wan22-t2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"t2v", "s2v"} })
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "s2v must be listed if and only if a workflow takes a sound file")
	errs = mutate(t, "wan22-s2v-14b", func(m *catalog.Model) { m.Traits.VideoModes = []traits.VideoMode{"r2v"} })
	require.Len(t, errs, 1, "the sound workflow of the S2V model has to say s2v")
	assert.Contains(t, errs[0].Error(), "s2v must be listed")
}
