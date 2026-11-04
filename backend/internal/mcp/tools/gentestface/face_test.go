package gentestface_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentestface"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

// ffInstall lays out a FaceFusion folder and a python in a temporary directory.
func ffInstall(t *testing.T) (dir, python string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the layout uses POSIX paths")
	}
	root := t.TempDir()
	dir = filepath.Join(root, "facefusion")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "facefusion"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, facefusion.Script), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion", "metadata.py"), []byte("METADATA =\\\n{\n\t'version': '3.9.2',\n\t'license': 'OpenRAIL-AS',\n}\n"), 0o600))
	python = filepath.Join(root, "python")
	require.NoError(t, os.WriteFile(python, nil, 0o700)) // #nosec G306 -- a test executable
	return dir, python
}

// rig is the MCP environment with the generation test (the tools of package gentest are there for get_generation_result and the job probe).
type rig struct {
	*mcptest.Env
	GT *gentesttest.Rig
}

func (r *rig) job(id string) mcpkit.JobStatus {
	r.T.Helper()
	return r.WaitJob(id, 20*time.Second, 10*time.Millisecond)
}

// anImage runs a batch of one image and returns its result id.
func (r *rig) anImage() string {
	r.T.Helper()
	ref, err := r.GT.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(r.T, err)
	return r.GT.Wait(ref.BatchID).Results[0].ID
}

// stageUploads stages n different uploads and returns them as the refs a tool takes.
func (r *rig) stageUploads(n int) []map[string]any {
	r.T.Helper()
	var out []map[string]any
	for i := range n {
		id, _, err := r.GT.Svc.StageReference(append(append([]byte(nil), gentesttest.PNG...), byte(i)))
		require.NoError(r.T, err)
		out = append(out, map[string]any{"source": "upload", "id": id})
	}
	return out
}

// newFaceFusionRig is the rig with the FaceFusion settings over a file of the test and a FaceFusion that returns a fixed picture.
func newFaceFusionRig(t *testing.T) (*rig, *facefusionsettings.Service) {
	t.Helper()
	ff := facefusionsettings.New(filepath.Join(t.TempDir(), facefusionsettings.FileName), "",
		facefusionsettings.WithFinder(facefusion.Finder{Finder: locate.Finder{GOOS: "linux", Path: "/nowhere", Usable: locate.IsExecutable}}))
	out := append(append([]byte(nil), gentesttest.PNG...), 9)
	swap := func(context.Context, runner.FaceSwapInput) ([]byte, string, error) { return out, "image/png", nil }
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB, gentesttest.WithFaceFusion(swap, func() error { _, err := ff.Look(); return err }))
		env.Services.Gentest = r.GT.Svc
		reg.Settings.FaceFusion = ff
		jobs.Register(reg, env.Services)
		files.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
		gentestface.Register(reg, env.Services)
	})
	return r, ff
}

func TestFaceFusionStatusIsReadThroughTheTool(t *testing.T) {
	dir, python := ffInstall(t)
	r, ff := newFaceFusionRig(t)

	var st gentestface.FaceFusionStatus
	r.CallOK("get_facefusion_status", map[string]any{}, &st)
	assert.False(t, st.Ready)
	assert.NotEmpty(t, st.Python.Problem, "nothing is found: it says so")
	assert.Equal(t, facefusion.Selection{Swapper: "ghost_2_256", Detector: "yunet", Enhancer: "gfpgan_1.4"}, st.Defaults)
	assert.NotEmpty(t, st.Swappers)
	assert.Contains(t, st.Enhancers[len(st.Enhancers)-1].ID, "none")
	assert.Equal(t, facefusion.LicensesURL, st.LicensesURL)
	assert.Contains(t, st.ArcFace, "ArcFace")
	var inswapper facefusion.Model
	for _, m := range st.Swappers {
		if m.ID == "inswapper_128" {
			inswapper = m
		}
	}
	assert.Equal(t, facefusion.ClassNonCommercial, inswapper.Class)

	require.NoError(t, ff.Set(facefusion.Config{Python: python, Path: dir})) // the paths are saved on the settings screen
	r.CallOK("get_facefusion_status", map[string]any{}, &st)
	assert.True(t, st.Ready)
	assert.Equal(t, "3.9.2", st.Version)
	assert.Equal(t, "OpenRAIL-AS", st.License)
	assert.Equal(t, python, st.Python.Configured)
}

// The paths of FaceFusion choose a program that is started later, so an MCP client (an agent that may have been prompted by what it read)
// does not set them: there is no tool for it (spec/api/mcp-tools-settings-policy.md, as for the paths of ffmpeg).
func TestNoToolSetsTheFaceFusionPaths(t *testing.T) {
	r, _ := newFaceFusionRig(t)

	listed, err := r.Client.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	assert.Contains(t, names, "get_facefusion_status")
	assert.Contains(t, names, "swap_generation_face")
	for _, name := range names {
		assert.NotContains(t, name, "facefusion_path", "a tool that sets the paths of FaceFusion")
	}
}

func TestSwapToolPutsTheFaceIntoAResultAndWarnsForANonCommercialModel(t *testing.T) {
	dir, python := ffInstall(t)
	r, ff := newFaceFusionRig(t)
	require.NoError(t, ff.Set(facefusion.Config{Python: python, Path: dir}))
	src := r.anImage()
	faces := r.stageUploads(1)

	var out gentestface.SwapFaceOutput
	r.CallOK("swap_generation_face", map[string]any{
		"target": map[string]any{"source": "history", "id": src}, "face": faces[0], "swapper": "inswapper_128", "enhancer": "none",
	}, &out)
	r.job(out.JobID)

	require.NotEmpty(t, out.ResultID)
	require.Len(t, out.Warnings, 1)
	assert.Contains(t, out.Warnings[0], "商用NG")
	var got record.ResultDetail
	r.CallOK("get_generation_result", map[string]any{"result_id": out.ResultID}, &got)
	assert.Equal(t, "succeeded", got.Status)
	assert.Equal(t, face.SwapModel, got.Model)
	require.NotNil(t, got.FaceSwap)
	assert.Equal(t, src, got.FaceSwap.SourceResultID)
	assert.Equal(t, "inswapper_128", got.FaceSwap.Swapper)
	assert.Equal(t, out.Warnings, got.FaceSwap.Warnings)
}

func TestSwapToolSaysFaceFusionIsNotSetUpAndRefusesAnUnknownModel(t *testing.T) {
	dir, python := ffInstall(t)
	r, ff := newFaceFusionRig(t)
	src := r.anImage()
	args := func(extra map[string]any) map[string]any {
		a := map[string]any{"target": map[string]any{"source": "history", "id": src}, "face": r.stageUploads(1)[0]}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}

	notFound := r.CallError("swap_generation_face", args(nil))
	assert.True(t, strings.HasPrefix(notFound, "unavailable: "), "FaceFusion is not found yet: a feature that is not set up (the class of the code facefusion_unavailable): %s", notFound)
	assert.Contains(t, notFound, `"code":"facefusion_unavailable"`, "the structure is on the second line (#1010)")
	assert.Contains(t, notFound, "設定画面の「FaceFusion」", "the remedy")
	assert.Contains(t, notFound, `"path":"/settings/facefusion"`)
	require.NoError(t, ff.Set(facefusion.Config{Python: python, Path: dir}))
	assert.Contains(t, r.CallError("swap_generation_face", args(map[string]any{"swapper": "nope"})), "validation")
}
