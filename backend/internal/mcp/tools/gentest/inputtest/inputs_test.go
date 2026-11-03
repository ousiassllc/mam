// Package inputtest_test tests the input images of the MCP tools of the generation test (#1009): the check is the one of the screen's estimate, and what a
// result was not given is recorded.
package inputtest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// The input images and the models that take them (#1009): estimate_generation_test and run_generation_test make the check the screen makes (the same
// Service.Estimate), so a request that cannot use its pictures is stopped with the common error, and one that loses some is warned about and records them.

type rig struct {
	*mcptest.Env
	GT *gentesttest.Rig
}

func newRig(t *testing.T, opts ...gentesttest.Option) *rig {
	t.Helper()
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB, opts...)
		env.Services.Gentest = r.GT.Svc
		jobs.Register(reg, env.Services)
		files.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
	})
	return r
}

func (r *rig) job(id string) mcpkit.JobStatus {
	r.T.Helper()
	return r.WaitJob(id, 20*time.Second, 10*time.Millisecond)
}

// stageUploads stages n uploads and returns their references as the tools take them.
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

func videoBatch(model string, refs []map[string]any, roles ...string) map[string]any {
	for i, role := range roles {
		refs[i]["role"] = role
	}
	return map[string]any{"media": "video", "models": []string{model}, "prompts": []map[string]any{{"prompt": "waves"}}, "references": refs[:len(roles)]}
}

func TestEstimateAndRunStopAnI2VModelWithoutAFirstFrame(t *testing.T) {
	r := newRig(t, gentesttest.WithInputModels())
	i2v := string(gentesttest.I2VModel)
	for _, tool := range []string{"estimate_generation_test", "run_generation_test"} {
		for name, args := range map[string]map[string]any{
			"no image":    videoBatch(i2v, nil),
			"a reference": videoBatch(i2v, r.stageUploads(1), "reference"),
		} {
			if tool == "run_generation_test" {
				args["estimate_id"], args["confirmed_total_usd"] = "x", 0
			}
			text := r.CallError(tool, args)
			assert.Contains(t, text, "validation", tool+" "+name)
			assert.Contains(t, text, `"code":"input_missing"`, "the structured failure is in the error text: "+tool+" "+name)
			assert.Contains(t, text, "最初のフレーム", tool+" "+name)
		}
	}
	text := r.CallError("estimate_generation_test", videoBatch(i2v, r.stageUploads(1), "reference"))
	assert.Contains(t, text, "添えた画像が「参照」になっています。「最初のフレーム」に変えてください")
	assert.Empty(t, r.GT.Fake.Calls())
}

func TestEstimateAndRunStopATextToVideoModelThatWouldUseNoImage(t *testing.T) {
	r := newRig(t, gentesttest.WithInputModels())
	args := videoBatch(string(gentesttest.T2VModel), r.stageUploads(1), "first_frame")
	for _, tool := range []string{"estimate_generation_test", "run_generation_test"} {
		if tool == "run_generation_test" {
			args["estimate_id"], args["confirmed_total_usd"] = "x", 0
		}
		text := r.CallError(tool, args)
		assert.Contains(t, text, `"code":"input_unused"`, tool)
		assert.Contains(t, text, "文章だけから作るモデルなので、添えた画像は使われません", tool)
	}
}

func TestAPartlyUnusedInputIsAWarningAndGetGenerationResultRecordsIt(t *testing.T) {
	r := newRig(t, gentesttest.WithInputModels())
	args := videoBatch(string(gentesttest.I2VModel), r.stageUploads(2), "first_frame", "last_frame")
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	var codes []string
	for _, w := range est.Warnings {
		codes = append(codes, w.Code)
	}
	assert.Contains(t, codes, gentest.WarnInputsDropped)

	args["estimate_id"], args["confirmed_total_usd"] = est.ID, est.TotalUSD
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", args, &out)
	r.job(out.JobID)
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 1)

	var got mcpgentest.ResultOutput
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &got)
	require.Len(t, got.UnusedInputs, 1)
	assert.Equal(t, "last_frame", got.UnusedInputs[0].Role)
	assert.Contains(t, got.UnusedInputs[0].Reason, "最後のフレーム")
	require.Len(t, got.Inputs, 2)
	var ids []string
	for _, in := range got.Inputs {
		ids = append(ids, in.ID)
	}
	assert.Contains(t, ids, got.UnusedInputs[0].InputID, "it names one of the result's inputs")
	require.Len(t, b.Results[0].UnusedInputs, 1, "get_generation_test carries it too")
}

func TestListModelsSaysWhichInputsAModelCannotRunWithout(t *testing.T) {
	r := newRig(t, gentesttest.WithInputModels())
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "video"}, &out)
	by := map[string]modelcatalog.Info{}
	for _, m := range out.Models {
		by[m.ID] = m
	}
	assert.Equal(t, []string{"first_frame"}, by[string(gentesttest.I2VModel)].RequiresInputs)
	assert.Equal(t, []string{"first_frame", "last_frame"}, by[string(gentesttest.FLFModel)].RequiresInputs)
	assert.Empty(t, by[string(gentesttest.T2VModel)].RequiresInputs)
}
