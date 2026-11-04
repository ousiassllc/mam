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
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// Issue #1026: while the face models are missing, an MCP client reads that the face similarity is off from list_generation_models before the run, from the warning
// of the estimate, and from face_similarity.skipped of the result after it.
func TestTheFaceSimilarityBeingOffIsReadFromTheToolsWhenTheFaceModelsAreMissing(t *testing.T) {
	off := face.OffRecord(&facesim.Off{Reason: facesim.ReasonModelsMissing, ModelDir: "/data/models", Missing: []string{facesim.DetectorFile, facesim.RecognizerFile}})
	var gt *gentesttest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		gt = gentesttest.NewOn(t, env.DB, gentesttest.WithReferenceModels(""), gentesttest.WithFaceOff(off))
		env.Services.Gentest = gt.Svc
		jobs.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
	})

	var models mcpgentest.ListModelsOutput
	env.CallOK("list_generation_models", map[string]any{"media": "image"}, &models)
	assert.False(t, models.FaceSimilarity.Enabled)
	require.NotNil(t, models.FaceSimilarity.Off)
	assert.Equal(t, record.SkipModelsMissing, models.FaceSimilarity.Off.Skipped)
	assert.Contains(t, models.FaceSimilarity.Off.Fix, "/data/models")

	id, _, err := gt.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	args := map[string]any{
		"media": "image", "models": []string{string(gentesttest.EditModel)}, "prompts": []map[string]any{{"prompt": "a fox"}},
		"references": []map[string]any{{"source": "upload", "id": id, "role": "face"}},
	}
	var est gentest.Estimate
	env.CallOK("estimate_generation_test", args, &est)
	warned := false
	for _, w := range est.Warnings {
		warned = warned || w.Code == record.WarnFaceSimilarityOff
	}
	assert.True(t, warned, "the estimate says it before the run")

	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	env.CallOK("run_generation_test", run, &out)
	require.Equal(t, "succeeded", string(env.WaitJob(out.JobID, 20*time.Second, 10*time.Millisecond).State))

	var b gentest.BatchStatus
	env.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 1)
	var res record.ResultDetail
	env.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &res)
	require.NotNil(t, res.FaceSimilarity, "the result carries face_similarity although the models are missing")
	assert.Equal(t, record.SkipModelsMissing, res.FaceSimilarity.Skipped)
	assert.Equal(t, []string{facesim.DetectorFile, facesim.RecognizerFile}, res.FaceSimilarity.MissingFiles)
	assert.NotEmpty(t, res.FaceSimilarity.Reason)
	assert.NotEmpty(t, res.FaceSimilarity.Fix)

	var raw map[string]any
	env.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &raw)
	fs, ok := raw["face_similarity"].(map[string]any)
	require.True(t, ok, "the JSON of the tool has face_similarity")
	assert.Equal(t, "models_missing", fs["skipped"])
}
