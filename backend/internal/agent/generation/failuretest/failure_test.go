package failuretest_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
)

func TestFailureReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"no model key", fmt.Errorf("x: %w", modeladapter.ErrNoAPIKey), "API キー"},
		{"no llm key", llm.ErrNoAPIKey, "API キー"},
		{"no llm key names the agent and the providers", &llm.RouteError{Agent: llm.AgentPromptPlanner, Tried: []llm.TargetFailure{
			{Target: llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-x"}, Err: llm.ErrNoAPIKey},
			{Target: llm.Target{Provider: llm.ProviderGoogle, Model: "gemini-x"}, Err: llm.ErrNoAPIKey},
		}}, "Prompt Planner（Shot Spec）"},
		{"budget", fmt.Errorf("x: %w", generation.ErrBudgetExceeded), "予算上限"},
		{"no usable model", fmt.Errorf("x: %w", &modelroute.NoUsableError{Media: "image", Key: "new_reference", Assigned: []modelroute.Problem{{Model: "nano-banana", Message: "API キーが登録されていません"}}}), "使えるモデルがありません"},
		{"brand", domain.ErrBrandViolation, "Brand"},
		{"judge off", generation.ErrJudgeUnavailable, "AI Video Judge"},
		{"time limit", fmt.Errorf("%w (1s)", generation.ErrGenerationTimeLimit), "制限時間"},
		{"rate limit", &modeladapter.ProviderError{Status: 429}, "レート制限"},
		{"circuit open", fmt.Errorf("x: %w", modeladapter.ErrCircuitOpen), "一時的に利用できません"},
		{"llm circuit open", fmt.Errorf("x: %w", llm.ErrCircuitOpen), "一時的に利用できません"},
		{"rejected", modeladapter.ErrRejected, "拒否"},
		{"no chatgpt image model", fmt.Errorf("x: %w", &modeladapter.ProviderError{Status: 400, Err: chatgpt.ErrNoImageModel}), "Codex のモデル"},
		{"model timeout", modeladapter.ErrTimeout, "時間内に返りませんでした"},
		{"llm timeout", llm.ErrTimeout, "時間内に返りませんでした"},
		{"ffmpeg timeout", fmt.Errorf("%w after 30s", media.ErrTimeout), "ffmpeg"},
		{"bad llm output", llm.ErrInvalidOutput, "求めた形式"},
		{"provider error", &modeladapter.ProviderError{Status: 502}, "障害が起きています"},
		{"llm api error", &llm.APIError{Status: 500}, "障害が起きています"},
		{"unknown", errors.New("sql: database is locked"), "原因がわかりませんでした"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := generation.FailureReason(c.err)
			assert.Contains(t, got, c.want)
			assert.NotContains(t, got, "sql:", "internal details are not shown to the user")
		})
	}
}

type failingVideo struct{ err error }

func (f failingVideo) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	return modeladapter.VideoResult{}, f.err
}

// A primary skipped for its open breaker must not make the user retry when the alternative refused: its reason is reported (#439).
func TestFailureReason_NamesTheAlternativesRejectionWhenThePrimaryIsSkippedByItsBreaker(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker.FailureThreshold = 1
	a := modeladapter.New(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: failingVideo{&modeladapter.ProviderError{Vendor: string(modeladapter.VendorSeedance), Status: 502}},
		domain.ModelKling3:    failingVideo{&modeladapter.ProviderError{Vendor: "fake", Status: 400, Message: "content policy"}},
	}}, modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
	seedance := modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"}
	kling := modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}
	job := modeladapter.VideoJob{Spec: domain.ShotSpec{ShotObjective: "run", Camera: domain.SpecCamera{Framing: "medium"}}, DurationSeconds: 4}
	_, err := a.GenerateVideo(context.Background(), seedance, seedance, job) // opens the Seedance breaker
	require.Error(t, err)

	_, err = a.GenerateVideo(context.Background(), seedance, kling, job)

	require.Error(t, err)
	assert.Contains(t, generation.FailureReason(err), "拒否")
	assert.NotContains(t, generation.FailureReason(err), "一時的に利用できません")
}

// The notice of a Shot that failed for want of an LLM key links to the LLM assignment; a model's missing key and other causes do not (#1006).
func TestFailureNotice_OnlyAMissingLLMKeyLinksToTheLLMAssignment(t *testing.T) {
	n := failmsg.Notice(&llm.RouteError{Agent: llm.AgentWriter, Tried: []llm.TargetFailure{{Err: llm.ErrNoAPIKey}}}, "text")
	assert.Equal(t, domain.RefGlobalSettings, n.RefType)
	assert.Equal(t, "llm", n.RefID)
	assert.Equal(t, domain.KindText, n.Kind)
	assert.Contains(t, generation.FailureReason(modeladapter.ErrNoAPIKey), "API キーとログイン")
	assert.Empty(t, failmsg.Notice(modeladapter.ErrNoAPIKey, "text").RefType)
	assert.Empty(t, failmsg.Notice(llm.ErrTimeout, "text").RefType)
}

// The failure of the pipeline is the common error (#1010): the code is what a screen and a tool branch on; the reason is its cause and remedy in Japanese.
func TestFailure_IsTheCommonErrorWithItsCodeAndRemedy(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		code   apperr.Code
		remedy string
	}{
		"no model key":      {fmt.Errorf("x: %w", modeladapter.ErrNoAPIKey), apperr.CodeKeyMissing, "API キーとログイン"},
		"no LLM key":        {llm.ErrNoAPIKey, apperr.CodeLLMKeyMissing, "API キーとログイン"},
		"budget":            {generation.ErrBudgetExceeded, apperr.CodeBudget, "予算の上限"},
		"time limit":        {fmt.Errorf("%w (1s)", generation.ErrGenerationTimeLimit), apperr.CodeGenTimeLimit, "制限時間を延ばす"},
		"VRAM":              {&modeladapter.ProviderError{Status: 422, Message: "CUDA out of memory"}, apperr.CodeVRAM, "解像度や秒数を小さく"},
		"not connected":     {&modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui"}, apperr.CodeConnectionFailed, "起動しているか"},
		"needs first_frame": {&modeladapter.ProviderError{Status: 422, Message: "the workflow needs first_frame (12), the request has none"}, apperr.CodeInputMissing, "first_frame"},
		"ffmpeg":            {fmt.Errorf("%w after 30s", media.ErrTimeout), apperr.CodeMediaTimeout, "素材ファイル"},
	} {
		t.Run(name, func(t *testing.T) {
			e := generation.Failure(tc.err)
			assert.Equal(t, tc.code, e.Code)
			assert.Contains(t, generation.FailureReason(tc.err), tc.remedy)
			assert.ErrorIs(t, e, tc.err, "the failure behind stays reachable for the log")
		})
	}
	unknown := generation.FailureReason(errors.New("sql: database is locked"))
	assert.NotContains(t, unknown, "sql:", "what the machine said is never in the reason of the chat")
}
