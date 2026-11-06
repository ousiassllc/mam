package configtest

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The offset of a frames assignment (Issue #1076) is a few frames added to the frame count; it belongs to frames only.
func TestFramesOffsetIsCheckedAtSave(t *testing.T) {
	validate := func(edit func(as []localcfg.Assignment) []localcfg.Assignment) []error {
		tpl, ok := localcfg.TemplateByID("txt2video-wan22-5b")
		require.True(t, ok)
		m := tpl.Model
		w := *m.Workflow
		w.Assignments = edit(slices.Clone(w.Assignments))
		m.Workflow = &w
		return localcfg.ValidateModel("box/offset", m, comfy())
	}
	setOffset := func(p localcfg.Param, offset int) func([]localcfg.Assignment) []localcfg.Assignment {
		return func(as []localcfg.Assignment) []localcfg.Assignment {
			i := slices.IndexFunc(as, func(a localcfg.Assignment) bool { return a.Param == p })
			require.GreaterOrEqual(t, i, 0)
			as[i].Offset = offset
			return as
		}
	}
	assert.Empty(t, validate(setOffset(localcfg.ParamFrames, -2)))
	assert.Empty(t, validate(setOffset(localcfg.ParamFrames, 64)))
	for name, edit := range map[string]func([]localcfg.Assignment) []localcfg.Assignment{
		"offset on the seed":       setOffset(localcfg.ParamSeed, -2),
		"offset below the bound":   setOffset(localcfg.ParamFrames, -65),
		"offset above the bound":   setOffset(localcfg.ParamFrames, 65),
		"offset on the frame rate": setOffset(localcfg.ParamFPS, 1),
	} {
		errs := validate(edit)
		require.NotEmpty(t, errs, name)
		assert.Contains(t, errs[0].Error(), "offset applies to frames only", name)
	}
}

func TestFramesForAddsTheOffsetAndKeepsAtLeastOneFrame(t *testing.T) {
	assert.Equal(t, 81, localcfg.Assignment{Param: localcfg.ParamFrames}.FramesFor(81))
	assert.Equal(t, 79, localcfg.Assignment{Param: localcfg.ParamFrames, Offset: -2}.FramesFor(81))
	assert.Equal(t, 5, localcfg.Assignment{Param: localcfg.ParamFrames, Offset: 4}.FramesFor(1))
	assert.Equal(t, 1, localcfg.Assignment{Param: localcfg.ParamFrames, Offset: -2}.FramesFor(1), "never below one frame")
}
