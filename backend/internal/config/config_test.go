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

func TestLoad_MalformedPortEnvironmentIsAnErrorNotTheDefault(t *testing.T) {
	for _, bad := range []string{"abc", "80a", "8 787", "1.5"} {
		_, err := config.Load(nil, env(map[string]string{"MAM_DATA_DIR": "/data", "MAM_PORT": bad}), io.Discard)
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), "MAM_PORT", bad)
	}
	cfg, err := config.Load(nil, env(map[string]string{"MAM_DATA_DIR": "/data"}), io.Discard)
	require.NoError(t, err)
	assert.Equal(t, config.DefaultPort, cfg.Port, "unset still means the default")
	cfg, err = config.Load(nil, env(map[string]string{"MAM_DATA_DIR": "/data", "MAM_PORT": "9001"}), io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 9001, cfg.Port)
}

func TestLoad_JudgeAndFaceModelSettings(t *testing.T) {
	cfg, err := config.Load([]string{"--data-dir", "/d"}, func(string) string { return "" }, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "/d/models", cfg.FaceModels(), "the models default to <data-dir>/models")
	assert.Empty(t, cfg.JudgeConfigPath)

	env := map[string]string{"MAM_JUDGE_CONFIG": "/j.json", "MAM_FACE_MODEL_DIR": "/m", "MAM_ONNXRUNTIME_LIB": "/lib/libonnxruntime.so"}
	cfg, err = config.Load([]string{"--data-dir", "/d"}, func(k string) string { return env[k] }, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "/j.json", cfg.JudgeConfigPath)
	assert.Equal(t, "/m", cfg.FaceModels())
	assert.Equal(t, "/lib/libonnxruntime.so", cfg.ONNXRuntimeLib)

	cfg, err = config.Load([]string{"--data-dir", "/d", "--judge-config", "/x.json", "--face-model-dir", "/y", "--onnxruntime-lib", "/z.so"}, func(k string) string { return env[k] }, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "/x.json", cfg.JudgeConfigPath, "a flag beats the environment")
	assert.Equal(t, "/y", cfg.FaceModels())
	assert.Equal(t, "/z.so", cfg.ONNXRuntimeLib)
}
