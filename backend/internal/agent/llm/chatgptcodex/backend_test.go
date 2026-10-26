package chatgptcodex_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/chatgptcodex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

type fixed struct {
	calls atomic.Int32
	text  string
}

func (f *fixed) Complete(context.Context, string, llm.Request) (string, error) {
	f.calls.Add(1)
	return f.text, nil
}

// rig is the whole path: an Agent of the Client, the backend, the Codex service and a fake app-server.
type rig struct {
	t        *testing.T
	fake     *codextest.Fake
	svc      *codex.Service
	fallback *fixed
	client   *llm.Client
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	fake := codextest.New(t, filepath.Join(dir, "codex-home"))
	svc := codex.New(codex.Options{
		DataDir: dir, Start: func(appserver.Launch) (appserver.Process, error) { return fake.Process(), nil },
	})
	t.Cleanup(svc.Close)
	exe := filepath.Join(dir, "codex-app-server")
	require.NoError(t, os.WriteFile(exe, []byte("x"), 0o700))
	require.NoError(t, svc.SetCustomPath(exe))

	cfg := llm.DefaultConfig()
	cfg.Routes[llm.AgentWriter] = llm.Route{
		Primary:  llm.Target{Provider: llm.ProviderChatGPTCodex, Model: "gpt-chosen"},
		Fallback: llm.Target{Provider: llm.ProviderOpenAI, Model: "gpt-x"},
	}
	require.NoError(t, cfg.Validate())
	fb := &fixed{text: "script from the api key"}
	backends := map[llm.Provider]llm.Backend{llm.ProviderOpenAI: fb}
	chatgptcodex.Add(backends, cfg, svc)
	require.Contains(t, backends, llm.ProviderChatGPTCodex)
	return &rig{t: t, fake: fake, svc: svc, fallback: fb,
		client: llm.NewClient(cfg, backends, llm.WithSleep(func(context.Context, time.Duration) error { return nil }))}
}

func (r *rig) write(ctx context.Context) (llm.Response, error) {
	return r.client.Complete(ctx, llm.AgentWriter, llm.Request{System: "writer", User: "write"})
}

func TestTheProviderIsKnownAndHasNoRouteByDefault(t *testing.T) {
	assert.Contains(t, llm.Providers(), llm.ProviderChatGPTCodex)
	assert.Equal(t, llm.Provider("chatgpt-codex"), llm.ProviderChatGPTCodex)
	cfg := llm.DefaultConfig()
	assert.Contains(t, cfg.Providers, llm.ProviderChatGPTCodex)
	for agent, route := range cfg.Routes {
		assert.NotEqual(t, llm.ProviderChatGPTCodex, route.Primary.Provider, agent)
		assert.NotEqual(t, llm.ProviderChatGPTCodex, route.Fallback.Provider, agent)
	}
	require.NoError(t, cfg.Validate())
}

func TestAnAgentOnTheCodexProviderGetsTheAnswerOfTheTurn(t *testing.T) {
	r := newRig(t)
	res, err := r.write(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "OK", res.Text)
	assert.Equal(t, llm.ProviderChatGPTCodex, res.Provider)
	assert.Equal(t, "gpt-chosen", res.Model)
	assert.False(t, res.Fallback)
	assert.Zero(t, r.fallback.calls.Load())
	assert.Equal(t, "gpt-chosen", r.fake.Threads()[0].Params.Model)
	assert.Equal(t, "writer", *r.fake.Threads()[0].Params.BaseInstructions)
}

func TestUsageLimitFailsOverWithoutRetryAndTheChatGetsTheReasonAndTheResetTime(t *testing.T) {
	r := newRig(t)
	reset := time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC)
	resetUnix := reset.Unix()
	r.fake.RateLimits = protocol.RateLimitsResult{RateLimits: protocol.RateLimitSnapshot{
		Primary: &protocol.RateLimitWindow{UsedPercent: 100, ResetsAt: &resetUnix},
	}}
	r.fake.Script = func(turn *codextest.Turn) {
		turn.Complete(protocol.TurnFailed, map[string]any{"message": "limit", "codexErrorInfo": "usageLimitExceeded"})
	}
	var notices []string
	ctx := llm.WithFallbackNotice(context.Background(), func(m string) { notices = append(notices, m) })
	// More calls than the breaker threshold: a usage limit is not an outage.
	for range 8 {
		res, err := r.write(ctx)
		require.NoError(t, err)
		assert.True(t, res.Fallback)
		assert.Equal(t, "script from the api key", res.Text)
	}
	assert.Len(t, r.fake.Threads(), 8, "every call tries the plan first (the breaker stays closed), once each (no retry)")
	require.NotEmpty(t, notices)
	assert.Contains(t, notices[0], "利用上限")
	assert.Contains(t, notices[0], "2026-10-25 18:30（日本時間）")
	assert.Contains(t, notices[0], "代替のプロバイダー")
}

func TestTransientFailuresAreRetriedAndPermanentOnesAreNot(t *testing.T) {
	cases := []struct {
		name    string
		info    any
		threads int
	}{
		{"connection", map[string]any{"httpConnectionFailed": map[string]any{}}, 3},
		{"stream", map[string]any{"responseStreamDisconnected": map[string]any{}}, 3},
		{"server error", "internalServerError", 3},
		{"context window", "contextWindowExceeded", 1},
		{"bad request", "badRequest", 1},
		{"sandbox", "sandboxError", 1},
		{"other", "other", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.fake.Script = func(turn *codextest.Turn) {
				turn.Complete(protocol.TurnFailed, map[string]any{"message": "m", "codexErrorInfo": c.info})
			}
			res, err := r.write(context.Background())
			require.NoError(t, err)
			assert.True(t, res.Fallback)
			assert.Len(t, r.fake.Threads(), c.threads)
		})
	}
}

func TestUnauthorizedAndNotSignedInAskForALoginAndGoToTheAlternative(t *testing.T) {
	r := newRig(t)
	r.fake.Script = func(turn *codextest.Turn) {
		turn.Complete(protocol.TurnFailed, map[string]any{"message": "401", "codexErrorInfo": "unauthorized"})
	}
	var notices []string
	ctx := llm.WithFallbackNotice(context.Background(), func(m string) { notices = append(notices, m) })
	res, err := r.write(ctx)
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Len(t, r.fake.Threads(), 1, "a login that must be repeated is not retried")
	require.Len(t, notices, 1)
	assert.Contains(t, notices[0], "ログインし直して")

	r = newRig(t)
	r.fake.Account = nil
	notices = nil
	ctx = llm.WithFallbackNotice(context.Background(), func(m string) { notices = append(notices, m) })
	res, err = r.write(ctx)
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Empty(t, r.fake.Threads())
	require.Len(t, notices, 1)
	assert.Contains(t, notices[0], "Codex 経由でログインしていません")
}

func TestAMissingEngineIsToldToTheChat(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.svc.SetCustomPath(""))
	var notice string
	ctx := llm.WithFallbackNotice(context.Background(), func(m string) { notice = m })
	res, err := r.write(ctx)
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Contains(t, notice, "エンジンが導入されていません")
}

func TestWhenEveryProviderFailedTheReasonIsInTheError(t *testing.T) {
	r := newRig(t)
	cfg := llm.DefaultConfig()
	cfg.Routes[llm.AgentWriter] = llm.Route{
		Primary:  llm.Target{Provider: llm.ProviderChatGPTCodex, Model: "m"},
		Fallback: llm.Target{Provider: llm.ProviderChatGPTCodex, Model: "m"},
	}
	r.fake.Account = nil
	backends := map[llm.Provider]llm.Backend{}
	chatgptcodex.Add(backends, cfg, r.svc)
	_, err := llm.NewClient(cfg, backends).Complete(context.Background(), llm.AgentWriter, llm.Request{User: "x"})
	var api *llm.APIError
	require.True(t, errors.As(err, &api))
	assert.Contains(t, api.Notice, "ログイン")
}

func TestACancelledCallIsACancelNotAFailure(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.fake.Script = func(*codextest.Turn) { cancel() }
	_, err := r.write(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, r.fallback.calls.Load(), "a cancelled call must not start the alternative")
}
