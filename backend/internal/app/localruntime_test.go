package app

import (
	"context"
	"log/slog"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// The production wiring of the local runtimes: buildServices makes the manager (GPT-SoVITS and MuseTalk are in the catalog) and it refuses a
// machine that cannot run it with the reason and the cloud fallback. On Windows with an NVIDIA GPU it is not refused, so that
// part is asserted only elsewhere (the CI runs Linux).
func TestBuildServices_TheLocalRuntimesAreWired(t *testing.T) {
	cfg := testConfig(t)
	services, _, cleanup, err := buildServices(context.Background(), cfg, slog.New(slog.DiscardHandler), testChatGPT(cfg), testFFmpegDL(cfg))
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NotNil(t, services.LocalRuntimes)

	views := services.LocalRuntimes.List(context.Background())
	require.Len(t, views, 5)
	assert.Equal(t, "gpt-sovits", views[0].ID)
	assert.Equal(t, "irodori-tts", views[1].ID)
	assert.Equal(t, "llama-cpp", views[2].ID)
	assert.Equal(t, "musetalk", views[3].ID)
	assert.Equal(t, "ollama", views[4].ID)
	if runtime.GOOS != "windows" {
		assert.False(t, views[0].Support.Supported)
		assert.Contains(t, views[0].Support.Message(), "ElevenLabs")
		_, err := services.LocalRuntimes.Install(context.Background(), "gpt-sovits")
		assert.ErrorContains(t, err, "自動導入に対応していません")
		assert.False(t, views[1].Support.Supported)
		assert.Contains(t, views[1].Support.Message(), "ElevenLabs")
		_, err = services.LocalRuntimes.Install(context.Background(), "irodori-tts")
		assert.ErrorContains(t, err, "自動導入に対応していません")
		assert.False(t, views[2].Support.Supported)
		assert.Contains(t, views[2].Support.Message(), "Ollama")
		_, err = services.LocalRuntimes.Install(context.Background(), "llama-cpp")
		assert.ErrorContains(t, err, "自動導入に対応していません")
		assert.False(t, views[3].Support.Supported)
		assert.Contains(t, views[3].Support.Message(), "Sync Labs")
		_, err = services.LocalRuntimes.Install(context.Background(), "musetalk")
		assert.ErrorContains(t, err, "自動導入")
		assert.False(t, views[4].Support.Supported)
		assert.Contains(t, views[4].Support.Message(), "別のサーバー")
		_, err = services.LocalRuntimes.Install(context.Background(), "ollama")
		assert.ErrorContains(t, err, "自動導入に対応していません")
	}
}

// "この PC の llama.cpp を使う" (#1008) is wired: the settings of the local LLM know the managed server, which says where it stands; on a PC that cannot run it
// it says so, and choosing it is possible (the calls then fail with the reason, not silently go to another address).
func TestBuildServices_TheManagedLlamaServerIsWiredToTheLocalLLMSettings(t *testing.T) {
	cfg := testConfig(t)
	services, _, cleanup, err := buildServices(context.Background(), cfg, slog.New(slog.DiscardHandler), testChatGPT(cfg), testFFmpegDL(cfg))
	require.NoError(t, err)
	t.Cleanup(cleanup)

	info, ok := services.LLMLocal.ManagedInfo(context.Background())

	require.True(t, ok, "the settings have a managed llama.cpp")
	assert.Equal(t, "gemma-4-12b-it", info.Model, "the default model")
	if runtime.GOOS != "windows" {
		assert.Equal(t, llmlocal.ManagedUnsupported, info.State)
		assert.Contains(t, info.Reason, "llama.cpp の自動導入に対応していません")
	}
	use := true
	require.NoError(t, services.LLMLocal.Save(llmlocal.SaveInput{UseManaged: &use}))
	_, _, err = services.LLMLocal.Endpoint(context.Background())
	if runtime.GOOS != "windows" {
		assert.ErrorContains(t, err, "llama.cpp の自動導入に対応していません")
	}
}
