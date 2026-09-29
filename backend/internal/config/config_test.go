package config_test

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(nil, env(map[string]string{"MAM_DATA_DIR": "/data"}), io.Discard)
	require.NoError(t, err)
	assert.Equal(t, config.DefaultPort, cfg.Port)
	assert.True(t, cfg.OpenBrowser)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
	assert.Equal(t, filepath.Join("/data", "mam.db"), cfg.DBPath())
	assert.Equal(t, filepath.Join("/data", "logs"), cfg.LogDir())
	assert.Equal(t, filepath.Join("/data", "assets"), cfg.AssetsDir())
}

func TestLoad_FlagsOverrideEnvironment(t *testing.T) {
	cfg, err := config.Load(
		[]string{"-port", "9000", "-no-browser", "-log-level", "debug", "-data-dir", "/flag"},
		env(map[string]string{"MAM_PORT": "1234", "MAM_DATA_DIR": "/env", "MAM_LOG_LEVEL": "error"}),
		io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 9000, cfg.Port)
	assert.False(t, cfg.OpenBrowser)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
	assert.Equal(t, "/flag", cfg.DataDir)
}

func TestLoad_EnvironmentIsUsedWhenNoFlag(t *testing.T) {
	cfg, err := config.Load(nil, env(map[string]string{
		"MAM_PORT": "1234", "MAM_DATA_DIR": "/env", "MAM_NO_BROWSER": "1", "MAM_STATIC_DIR": "/static", "MAM_LLM_CONFIG": "/llm.json", "MAM_MODEL_CONFIG": "/models.json",
	}), io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 1234, cfg.Port)
	assert.False(t, cfg.OpenBrowser)
	assert.Equal(t, "/static", cfg.StaticDir)
	assert.Equal(t, "/llm.json", cfg.LLMConfigPath)
	assert.Equal(t, "/models.json", cfg.ModelConfigPath)
}

func TestLoad_RejectsInvalidValues(t *testing.T) {
	base := map[string]string{"MAM_DATA_DIR": "/data"}
	_, err := config.Load([]string{"-log-level", "loud"}, env(base), io.Discard)
	assert.Error(t, err)
	_, err = config.Load([]string{"-port", "70000"}, env(base), io.Discard)
	assert.Error(t, err)
	_, err = config.Load([]string{"-nope"}, env(base), io.Discard)
	assert.Error(t, err)
}
