package gentestface_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest/facestatus"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentestface"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels/facemodelstest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// facesRig is the generation test of an MCP client with the face similarity wired the way the app wires it: the score and the "why it is off" ask the engine
// holder, which reads the model folder the face models are installed to (the fake GitHub and the fake ONNX engine of facemodelstest).
type facesRig struct {
	env *mcptest.Env
	gt  *gentesttest.Rig
	fm  *facemodelstest.Rig
	// last is the batch get_generation_test returned for the latest run.
	last gentest.BatchStatus
}

func newFacesRig(t *testing.T) *facesRig {
	t.Helper()
	r := &facesRig{fm: facemodelstest.New(t)}
	r.env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.gt = gentesttest.NewOn(t, env.DB, gentesttest.WithReferenceModels(""),
			gentesttest.WithFaceScore(face.Score(r.fm.Engine)), gentesttest.WithFaceOffFunc(face.OffNow(r.fm.Engine.Off)), gentesttest.WithFaceModels(r.fm.Svc))
		env.Services.Gentest = r.gt.Svc
		jobs.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
		gentestface.Register(reg, env.Services)
	})
	return r
}

// warnings are the codes of the estimate's warnings for a request with a face reference.
func (r *facesRig) warnings(t *testing.T) []string {
	t.Helper()
	id, _, err := r.gt.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	var est gentest.Estimate
	r.env.CallOK("estimate_generation_test", map[string]any{
		"media": "image", "models": []string{string(gentesttest.EditModel)}, "prompts": []map[string]any{{"prompt": "a fox"}},
		"references": []map[string]any{{"source": "upload", "id": id, "role": "face"}},
	}, &est)
	var codes []string
	for _, w := range est.Warnings {
		codes = append(codes, w.Code)
	}
	return codes
}

// run generates the same request every time: one image from a face reference, and returns the face_similarity the result carries (nil: none).
func (r *facesRig) run(t *testing.T) *record.FaceSimilarity {
	t.Helper()
	id, _, err := r.gt.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	args := map[string]any{
		"media": "image", "models": []string{string(gentesttest.EditModel)}, "prompts": []map[string]any{{"prompt": "a fox"}},
		"references": []map[string]any{{"source": "upload", "id": id, "role": "face"}},
	}
	var est gentest.Estimate
	r.env.CallOK("estimate_generation_test", args, &est)
	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	r.env.CallOK("run_generation_test", run, &out)
	require.Equal(t, "succeeded", string(r.env.WaitJob(out.JobID, 20*time.Second, 10*time.Millisecond).State))
	var b gentest.BatchStatus
	r.env.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 1)
	r.last = b
	var res record.ResultDetail
	r.env.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &res)
	return res.FaceSimilarity
}

func (r *facesRig) status(t *testing.T) facestatus.Status {
	t.Helper()
	var models mcpgentest.ListModelsOutput
	r.env.CallOK("list_generation_models", map[string]any{"media": "image"}, &models)
	return models.FaceSimilarity
}

// Issue #1026, the acceptance: from "not installed", install the face check models, run the same generation test, and the result carries the number of the face
// similarity, without a restart. Not installed, the model list says so and the result says why; after a removal it is off again.
func TestFromNotInstalledToInstalledTheSameGenerationTestGetsAFaceSimilarityNumberWithoutARestart(t *testing.T) {
	r := newFacesRig(t)

	// not installed: the state is readable from the model list, and the result says why there is no number and where to fix it
	before := r.status(t)
	assert.False(t, before.Enabled)
	require.NotNil(t, before.Models)
	assert.Equal(t, facemodels.StateMissing, before.Models.State)
	assert.False(t, before.Models.Installed)
	assert.Equal(t, facemodels.ToolName, before.Models.InstallTool)
	assert.Equal(t, "models_missing", before.Models.EngineOff)
	require.Len(t, before.Models.Files, 2)
	require.NotEmpty(t, before.Models.Licenses)
	require.NotNil(t, before.Off)
	assert.Contains(t, before.Off.Fix, "顔チェック用モデル")
	assert.Contains(t, before.Off.Fix, "/model-manager")
	assert.Contains(t, r.warnings(t), record.WarnFaceSimilarityOff, "the confirmation warns before the run")
	skipped := r.run(t)
	require.NotNil(t, skipped, "the result carries face_similarity although the models are missing")
	assert.Equal(t, record.SkipModelsMissing, skipped.Skipped)
	assert.Zero(t, skipped.Cosine)

	// installing needs the agreement of the user: without it nothing is fetched
	msg := r.env.CallError("install_face_models", map[string]any{"accepted": false})
	assert.Contains(t, msg, "validation")
	assert.Zero(t, r.fm.Requests.Load())

	var installed facemodels.View
	r.env.CallOK("install_face_models", map[string]any{"accepted": true}, &installed)
	assert.Equal(t, facemodels.StateInstalled, installed.State)
	assert.True(t, installed.Installed)
	assert.True(t, installed.EngineReady)
	assert.Empty(t, installed.EngineOff)

	// the same conditions: now a number, with no restart of anything
	after := r.status(t)
	assert.True(t, after.Enabled)
	assert.Nil(t, after.Off)
	require.NotNil(t, after.Models)
	assert.True(t, after.Models.Installed)
	assert.NotContains(t, r.warnings(t), record.WarnFaceSimilarityOff, "the warning is gone with the models, without a restart")
	scored := r.run(t)
	require.NotNil(t, scored)
	assert.Empty(t, scored.Skipped)
	assert.InDelta(t, 1.0, scored.Cosine, 1e-6, "the fake engine gives every face the same embedding")
	assert.True(t, scored.SamePerson)
	assert.Equal(t, facesim.SameFaceThreshold, scored.Threshold)
	// get_generation_test reads the numbers of the batch together per model (Issue #998)
	require.Len(t, r.last.FaceSummaries, 1)
	assert.Equal(t, 1, r.last.FaceSummaries[0].Measured)
	assert.InDelta(t, 1.0, r.last.FaceSummaries[0].MeanCosine, 1e-6)
	assert.Equal(t, 1, r.last.FaceSummaries[0].SamePerson)

	// removing turns it off again, and says so in the same words
	var removed facemodels.View
	r.env.CallOK("remove_face_models", map[string]any{}, &removed)
	assert.Equal(t, facemodels.StateMissing, removed.State)
	off := r.run(t)
	require.NotNil(t, off)
	assert.Equal(t, record.SkipModelsMissing, off.Skipped)
	assert.False(t, r.status(t).Enabled)
}

// The ONNX Runtime library is a separate reason: with the models installed and the library missing, the status says so and does not offer the install as the fix.
func TestTheFaceModelsInstalledButTheONNXRuntimeMissingIsAnotherReason(t *testing.T) {
	r := newFacesRig(t)
	r.fm.RuntimeMissing.Store(true)
	var installed facemodels.View
	r.env.CallOK("install_face_models", map[string]any{"accepted": true}, &installed)
	assert.True(t, installed.Installed)
	assert.False(t, installed.EngineReady)

	st := r.status(t)
	assert.False(t, st.Enabled)
	require.NotNil(t, st.Models)
	assert.Equal(t, "runtime_unavailable", st.Models.EngineOff)
	assert.Empty(t, st.Models.InstallTool)
	require.NotNil(t, st.Off)
	assert.Equal(t, record.SkipRuntimeMissing, st.Off.Skipped)
	assert.Contains(t, st.Off.Fix, "ONNX Runtime")
	assert.Contains(t, st.Off.Fix, "導入し直しても直りません", "the fix does not send the user to the model manager for a missing library")
	res := r.run(t)
	require.NotNil(t, res)
	assert.Equal(t, record.SkipRuntimeMissing, res.Skipped)

	r.fm.RuntimeMissing.Store(false) // the library is put in place: the next result is measured, no restart
	assert.Empty(t, r.run(t).Skipped)
}

// A tool of an app without the models answers "unavailable", and list_generation_models has no models block.
func TestTheFaceModelsToolsAreUnavailableWithoutTheModelsService(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		gt := gentesttest.NewOn(t, env.DB, gentesttest.WithReferenceModels(""))
		env.Services.Gentest = gt.Svc
		mcpgentest.Register(reg, env.Services)
		gentestface.Register(reg, env.Services)
	})
	assert.Contains(t, env.CallError("install_face_models", map[string]any{"accepted": true}), "unavailable")
	assert.Contains(t, env.CallError("remove_face_models", map[string]any{}), "unavailable")
	var models mcpgentest.ListModelsOutput
	env.CallOK("list_generation_models", map[string]any{"media": "image"}, &models)
	assert.Nil(t, models.FaceSimilarity.Models)
}
