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
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
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
		{"budget", fmt.Errorf("x: %w", generation.ErrBudgetExceeded), "予算上限"},
		{"brand", domain.ErrBrandViolation, "Brand"},
		{"judge off", generation.ErrJudgeUnavailable, "AI Video Judge"},
		{"time limit", fmt.Errorf("%w (1s)", generation.ErrGenerationTimeLimit), "制限時間"},
		{"rate limit", &modeladapter.ProviderError{Status: 429}, "レート制限"},
		{"circuit open", fmt.Errorf("x: %w", modeladapter.ErrCircuitOpen), "一時的に利用できません"},
		{"llm circuit open", fmt.Errorf("x: %w", llm.ErrCircuitOpen), "AI サービスが一時的に利用できません"},
		{"rejected", modeladapter.ErrRejected, "拒否"},
		{"no chatgpt image model", fmt.Errorf("x: %w", &modeladapter.ProviderError{Status: 400, Err: chatgpt.ErrNoImageModel}), "Codex のモデル"},
		{"model timeout", modeladapter.ErrTimeout, "タイムアウト"},
		{"llm timeout", llm.ErrTimeout, "タイムアウト"},
		{"ffmpeg timeout", fmt.Errorf("%w after 30s", media.ErrTimeout), "ffmpeg"},
		{"bad llm output", llm.ErrInvalidOutput, "AI の出力"},
		{"provider error", &modeladapter.ProviderError{Status: 502}, "生成モデルの呼び出しに失敗"},
		{"llm api error", &llm.APIError{Status: 500}, "AI サービスの呼び出しに失敗"},
		{"unknown", errors.New("sql: database is locked"), "内部エラー"},
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
