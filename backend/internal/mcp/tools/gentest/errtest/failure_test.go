package errtest_test

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// rig is the MCP environment with the generation test and its tools.
type rig struct {
	*mcptest.Env
	GT *gentesttest.Rig
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB)
		env.Services.Gentest = r.GT.Svc
		jobs.Register(reg, env.Services)
		files.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
	})
	return r
}

// run estimates a batch of one image and runs it with the figures it was shown.
func (r *rig) run(model string) mcpgentest.RunOutput {
	r.T.Helper()
	args := map[string]any{"media": "image", "models": []string{model}, "count": 1, "prompts": []map[string]any{{"prompt": "a red fox"}}}
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	args["estimate_id"], args["confirmed_total_usd"] = est.ID, est.TotalUSD
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", args, &out)
	return out
}

func comfyFailure(status int, msg string) error {
	return &modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Status: status, Message: msg}
}

// The failures of the issue (#1010) come out of get_generation_result as the common error: the code, the cause and the remedy in Japanese,
// the screens that help and the detail, and get_generation_test / list_generation_results carry the same.
func TestGetGenerationResultTellsAFailureWithCauseRemedyAndDetail(t *testing.T) {
	for name, tc := range map[string]struct {
		err      error
		code     apperr.Code
		cause    string
		remedy   string
		settings string
	}{
		"422 needs first_frame": {comfyFailure(422, "the workflow needs first_frame (12), the request has none"), apperr.CodeInputMissing, "first_frame", "最初のフレーム", ""},
		"LLM key not set":       {fmt.Errorf("translate: %w", llm.ErrNoAPIKey), apperr.CodeLLMKeyMissing, "LLM の API キーが設定されていません", "API キーを登録", "/settings/api-keys"},
		"ComfyUI not connected": {&modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, apperr.CodeConnectionFailed, "つながりませんでした", "起動している", "/model-manager"},
		"out of VRAM":           {comfyFailure(422, "KSamplerAdvanced: CUDA out of memory. Tried to allocate 3.20 GiB"), apperr.CodeVRAM, "VRAM", "解像度や秒数を小さく", ""},
		"model not installed":   {comfyFailure(400, "Value not in list: unet_name: 'wan2.2_i2v.safetensors' not in []"), apperr.CodeModelMissing, "モデルのファイル", "モデル管理", "/model-manager"},
		"unknown":               {errors.New("something nobody knows"), apperr.CodeUnknown, "原因がわかりませんでした", "もう一度", ""},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.GT.Fake.FailWith(domain.ModelFlux2, tc.err)
			out := r.run("flux-2")
			r.WaitJob(out.JobID, 20*time.Second, 10*time.Millisecond) // the job ends; the results say which failed

			var b gentest.BatchStatus
			r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
			require.NotEmpty(t, b.Results)
			require.NotNil(t, b.Results[0].Failure, "get_generation_test carries it too")
			assert.Equal(t, tc.code, b.Results[0].Failure.Code)

			var got mcpgentest.ResultOutput
			r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &got)
			require.NotNil(t, got.Failure)
			assert.Equal(t, tc.code, got.Failure.Code)
			assert.NotEmpty(t, got.Failure.Class)
			assert.Contains(t, got.Failure.Cause, tc.cause)
			assert.Contains(t, got.Failure.Remedy, tc.remedy)
			assert.Contains(t, got.Failure.Detail, tc.err.Error()[:15], "the original message is the detail")
			if tc.settings != "" {
				var paths []string
				for _, l := range got.Failure.Settings {
					paths = append(paths, l.Path)
				}
				assert.Contains(t, paths, tc.settings)
			}
			if tc.code == apperr.CodeUnknown {
				assert.NotEmpty(t, got.Failure.ErrorID, "an unknown failure has the id of its log line")
			}
			assert.Contains(t, got.Error, got.Failure.Cause, "error stays: the one-line text")
		})
	}
}
