package gentestposes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentestposes"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
)

type rig struct {
	*mcptest.Env
	Lib *posestest.Rig
}

func newRig(t *testing.T, withLibrary bool) *rig {
	t.Helper()
	r := &rig{Lib: posestest.New(t)}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		opt := gentesttest.WithPoseLibrary(nil)
		if withLibrary {
			opt = gentesttest.WithPoseLibrary(r.Lib.Lib)
		}
		env.Services.Gentest = gentesttest.NewOn(t, env.DB, opt).Svc
		gentestposes.Register(reg, env.Services)
	})
	return r
}

// The library is installed only with the agreement, searched by its words, and deleted again; a refusal is an error code and fetches nothing.
func TestThePoseLibraryIsInstalledWithTheAgreementSearchedAndRemoved(t *testing.T) {
	r := newRig(t, true)
	var lib gentestposes.Library
	r.CallOK("get_pose_library", map[string]any{}, &lib)
	assert.False(t, lib.Installed)
	assert.NotEmpty(t, lib.Source)
	assert.NotEmpty(t, lib.Download)
	require.NotEmpty(t, lib.Licenses)
	assert.Equal(t, "yes", lib.Licenses[0].Commercial)
	assert.Empty(t, lib.Poses)

	assert.Contains(t, r.CallError("install_pose_library", map[string]any{"accepted": false}), string(mcpkit.CodeValidation))
	assert.Zero(t, r.Lib.Requests.Load(), "no agreement, nothing fetched")

	r.CallOK("install_pose_library", map[string]any{"accepted": true}, &lib)
	assert.True(t, lib.Installed)
	var ids []string
	for _, p := range lib.Poses {
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []string{"P001", "P002", "U001"}, ids, "no suggestive pose, no pose without a picture")
	assert.Equal(t, "std_P001_front", lib.Poses[0].Images["front"])
	// The depth and normal maps of the angles that have one, as references to a ControlNet.
	assert.Equal(t, map[string]string{"front": "std_P001_front.depth"}, lib.Poses[0].DepthImages)
	assert.Equal(t, map[string]string{"front": "std_P001_front.normal", "diag": "std_P001_diag.normal"}, lib.Poses[0].NormalImages)
	assert.Empty(t, lib.Poses[2].DepthImages, "a pose with no map lists none")

	var found gentestposes.Library
	r.CallOK("get_pose_library", map[string]any{"query": "sitting"}, &found)
	require.Len(t, found.Poses, 1)
	assert.Equal(t, "P001", found.Poses[0].ID)
	r.CallOK("get_pose_library", map[string]any{"series": "upper"}, &found)
	require.Len(t, found.Poses, 1)
	assert.Equal(t, "U001", found.Poses[0].ID)

	var gone gentestposes.Library
	r.CallOK("remove_pose_library", map[string]any{}, &gone)
	assert.False(t, gone.Installed)
	assert.Empty(t, gone.Poses)
	r.CallOK("remove_pose_library", map[string]any{}, nil) // gone already: no error
}

func TestWithoutAPoseLibraryTheToolsAnswerUnavailable(t *testing.T) {
	r := newRig(t, false)
	for tool, args := range map[string]map[string]any{"get_pose_library": {}, "install_pose_library": {"accepted": true}, "remove_pose_library": {}} {
		assert.Contains(t, r.CallError(tool, args), string(mcpkit.CodeUnavailable), tool)
	}
}
