package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
)

func TestSettingsFileToLoad_AMissingDefaultFileSelectsTheDefaults(t *testing.T) {
	cfg := testConfig(t)

	file, err := settingsFileToLoad(cfg.LLMSettingsFile())
	require.NoError(t, err)
	assert.Empty(t, file)
	loaded, err := llm.LoadConfig(file)
	require.NoError(t, err)
	assert.Equal(t, llm.DefaultConfig(), loaded)

	file, err = settingsFileToLoad(cfg.ModelSettingsFile())
	require.NoError(t, err)
	assert.Empty(t, file)
	_, err = configcheck.Load(file)
	require.NoError(t, err)
}

func TestSettingsFileToLoad_AnExistingDefaultFileOverridesTheDefaults(t *testing.T) {
	cfg := testConfig(t)
	llmPath, pinned := cfg.LLMSettingsFile()
	require.False(t, pinned)
	require.NoError(t, os.WriteFile(llmPath, []byte(`{"retry": {"max_attempts": 7, "base_delay_ms": 10, "max_delay_ms": 100}}`), 0o600))
	modelPath, pinned := cfg.ModelSettingsFile()
	require.False(t, pinned)
	require.NoError(t, os.WriteFile(modelPath, []byte(`{"vendors":{"kling":{"timeout_seconds":300}}}`), 0o600))

	file, err := settingsFileToLoad(cfg.LLMSettingsFile())
	require.NoError(t, err)
	assert.Equal(t, llmPath, file)
	loaded, err := llm.LoadConfig(file)
	require.NoError(t, err)
	assert.Equal(t, 7, loaded.Retry.MaxAttempts)

	file, err = settingsFileToLoad(cfg.ModelSettingsFile())
	require.NoError(t, err)
	assert.Equal(t, modelPath, file)
	models, err := configcheck.Load(file)
	require.NoError(t, err)
	assert.Equal(t, 300, models.Vendors["kling"].TimeoutSeconds)
}

func TestSettingsFileToLoad_APinnedFileIsLoadedAsIsSoAMissingOneFails(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLMConfigPath = filepath.Join(t.TempDir(), "missing.json")

	file, err := settingsFileToLoad(cfg.LLMSettingsFile())
	require.NoError(t, err)
	assert.Equal(t, cfg.LLMConfigPath, file)
	_, err = llm.LoadConfig(file)
	assert.Error(t, err)
}

func TestSettingsFileToLoad_ADefaultFileThatCannotBeReadIsAnError(t *testing.T) {
	cfg := testConfig(t)
	path, _ := cfg.LLMSettingsFile()
	require.NoError(t, os.Mkdir(path, 0o700)) // a directory is neither missing nor a settings file

	file, err := settingsFileToLoad(cfg.LLMSettingsFile())
	require.NoError(t, err)
	_, err = llm.LoadConfig(file)
	assert.Error(t, err)
}

func TestBuildServices_TheDefaultSettingsFilesOfTheDataDirAreLoadedAtStartup(t *testing.T) {
	for name, content := range map[string]string{
		"llm.json":    `{"typo": 1}`,
		"models.json": `{"typo": 1}`,
	} {
		cfg := testConfig(t)
		require.NoError(t, os.WriteFile(filepath.Join(cfg.DataDir, name), []byte(content), 0o600))

		services, _, cleanup, err := buildServices(context.Background(), cfg, slog.New(slog.DiscardHandler), testChatGPT(cfg), testFFmpegDL(cfg))
		require.Error(t, err, name)
		assert.Nil(t, services, name)
		assert.Nil(t, cleanup, name)
	}

	cfg := testConfig(t)
	require.NoError(t, os.WriteFile(filepath.Join(cfg.DataDir, "llm.json"), []byte(`{"timeout_seconds": 90}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.DataDir, "models.json"), []byte(`{}`), 0o600))
	_, _, cleanup, err := buildServices(context.Background(), cfg, slog.New(slog.DiscardHandler), testChatGPT(cfg), testFFmpegDL(cfg))
	require.NoError(t, err)
	cleanup()
}
