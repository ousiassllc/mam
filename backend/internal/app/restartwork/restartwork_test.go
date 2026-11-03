package restartwork_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/app/restartwork"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

// llmRig is the LLM assignment of the screen over a running client, wired as the app does (#1006).
type llmRig struct {
	client  *llm.Client
	svc     *llmassign.Service
	pending *restartpending.Registry
	file    string
}

func newLLMRig(t *testing.T) *llmRig {
	t.Helper()
	r := &llmRig{pending: restartpending.New(), file: filepath.Join(t.TempDir(), "llm.json")}
	r.client = llm.NewClient(llm.DefaultConfig(), nil)
	r.svc = llmassign.New(r.file, false)
	r.svc.SetApplier(restartwork.LLMApplier(r.client, r.pending))
	return r
}

func route(provider, model string) llmassign.Route {
	t := llmassign.Target{Provider: provider, Model: model}
	return llmassign.Route{Primary: t, Fallback: t}
}

// The scene of #1006: the routes were saved (set_llm_assignment) while the app ran with the old ones. A saved assignment reaches the
// running client at once, so the next call goes to the new provider, and nothing is left waiting for a restart.
func TestLLMApplier_ASavedAssignmentIsInEffectAtOnceWithoutARestart(t *testing.T) {
	r := newLLMRig(t)
	require.Equal(t, llm.Target{Provider: llm.ProviderOpenAI, Model: "gpt-5"}, r.client.PrimaryOf(llm.AgentProducer), "the default route runs first")

	require.NoError(t, r.svc.Save(map[string]llmassign.Route{string(llm.AgentProducer): route("chatgpt-subscription", "gpt-6.1-sol")}))

	assert.Equal(t, llm.Target{Provider: llm.ProviderChatGPTSubscription, Model: "gpt-6.1-sol"}, r.client.PrimaryOf(llm.AgentProducer), "the running client has the saved route")
	assert.Empty(t, r.pending.Reasons(), "nothing waits for a restart")
	assert.True(t, r.svc.View().Live)
}

// What only a restart applies is listed, with the words the screen and get_health show; a saved ffmpeg path is one.
func TestLLMApplier_WhatOnlyARestartAppliesIsListedAsPending(t *testing.T) {
	r := newLLMRig(t)
	raw, err := json.Marshal(map[string]any{"timeout_seconds": 300}) // llm.json edited by hand
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(r.file, raw, 0o600))

	require.NoError(t, r.svc.Save(map[string]llmassign.Route{string(llm.AgentWriter): route("google", "m")}))

	assert.Equal(t, []string{"llm.json のタイムアウト・リトライ・サーキットブレーカー"}, r.pending.Reasons())
	assert.Equal(t, []string{"タイムアウト・リトライ・サーキットブレーカー"}, r.svc.View().Pending)

	restartwork.FFmpegPaths(r.pending)() // the paths of ffmpeg are found at startup, so a saved one waits too
	assert.Equal(t, []string{"ffmpeg・ffprobe のパス（起動時に探すため）", "llm.json のタイムアウト・リトライ・サーキットブレーカー"}, r.pending.Reasons(), "in the order of the keys")

	require.NoError(t, os.Remove(r.file)) // the file is back to the defaults: the entry goes
	require.NoError(t, r.svc.Reset())
	assert.Equal(t, []string{"ffmpeg・ffprobe のパス（起動時に探すため）"}, r.pending.Reasons())
}

// What a restart would interrupt is read from the job table (an install is a download); an idle app has none.
func TestBusy_ReadsTheJobTable(t *testing.T) {
	db := testdb.Open(t)
	services := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New()})
	jobs := repository.NewJobRepository(db)
	busy := restartwork.Busy(services, jobs, ffmpegdl.New(ffmpegdl.Options{Logger: slog.New(slog.DiscardHandler)}))
	assert.Empty(t, busy(context.Background()), "an idle app")

	_, err := jobs.Create(context.Background(), domain.JobGenerate, json.RawMessage(`{}`))
	require.NoError(t, err)
	got := busy(context.Background())
	require.Len(t, got, 1)
	assert.Equal(t, "jobs", got[0].Kind)
	assert.Contains(t, got[0].Message, "1 件")

	_, err = jobs.Create(context.Background(), domain.JobModelInstall, json.RawMessage(`{}`))
	require.NoError(t, err)
	var kinds []string
	for _, b := range busy(context.Background()) {
		kinds = append(kinds, b.Kind)
	}
	assert.Equal(t, []string{"jobs", "download"}, kinds, "a model install is a download: 'stop' does not cut it")
}
