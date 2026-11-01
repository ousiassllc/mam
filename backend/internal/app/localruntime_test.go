package app

import (
	"context"
	"log/slog"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.Len(t, views, 2)
	assert.Equal(t, "gpt-sovits", views[0].ID)
	assert.Equal(t, "musetalk", views[1].ID)
	if runtime.GOOS != "windows" {
		assert.False(t, views[0].Support.Supported)
		assert.Contains(t, views[0].Support.Message(), "ElevenLabs")
		_, err := services.LocalRuntimes.Install(context.Background(), "gpt-sovits")
		assert.ErrorContains(t, err, "自動導入に対応していません")
		assert.False(t, views[1].Support.Supported)
		assert.Contains(t, views[1].Support.Message(), "Sync Labs")
		_, err = services.LocalRuntimes.Install(context.Background(), "musetalk")
		assert.ErrorContains(t, err, "自動導入")
	}
}
