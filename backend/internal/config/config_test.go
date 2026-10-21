package config_test

import (
	"flag"
	"io"
	"log/slog"
	"os"
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

func TestLoad_MalformedPortEnvironmentDoesNotBreakHelpOrThePortFlag(t *testing.T) {
	bad := env(map[string]string{"MAM_DATA_DIR": "/data", "MAM_PORT": "abc"})

	_, err := config.Load([]string{"--help"}, bad, io.Discard)
	assert.ErrorIs(t, err, flag.ErrHelp)

	cfg, err := config.Load([]string{"--port", "9000"}, bad, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 9000, cfg.Port, "an explicit --port wins over the environment, valid or not")

	cfg, err = config.Load([]string{"--port", "0"}, bad, io.Discard)
	require.NoError(t, err)
	assert.Zero(t, cfg.Port, "--port 0 is explicit too")
}

func TestLoad_JudgeAndFaceModelSettings(t *testing.T) {
	cfg, err := config.Load([]string{"--data-dir", "/d"}, func(string) string { return "" }, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/d", "models"), cfg.FaceModels(), "the models default to <data-dir>/models")
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

// noUserConfigDir makes os.UserConfigDir fail: no XDG_CONFIG_HOME, no HOME (a systemd unit, a minimal container).
func noUserConfigDir(t *testing.T) {
	t.Helper()
	for _, k := range []string{"XDG_CONFIG_HOME", "HOME", "AppData"} {
		t.Setenv(k, "")
	}
	if _, err := os.UserConfigDir(); err == nil {
		t.Skip("the OS resolves a config directory without these variables")
	}
}

func TestLoad_ADataDirGivenByFlagOrEnvironmentNeedsNoDefault(t *testing.T) {
	noUserConfigDir(t)

	cfg, err := config.Load([]string{"--data-dir", "/var/lib/mam"}, env(nil), io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "/var/lib/mam", cfg.DataDir)

	cfg, err = config.Load(nil, env(map[string]string{"MAM_DATA_DIR": "/env"}), io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "/env", cfg.DataDir)

	cfg, err = config.Load([]string{"--data-dir", "/flag"}, env(map[string]string{"MAM_DATA_DIR": "/env"}), io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "/flag", cfg.DataDir, "the flag wins")
}

func TestLoad_WithoutADataDirAndWithoutADefaultItFailsButHelpStillWorks(t *testing.T) {
	noUserConfigDir(t)

	_, err := config.Load(nil, env(nil), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user config dir")

	_, err = config.Load([]string{"--help"}, env(nil), io.Discard)
	assert.ErrorIs(t, err, flag.ErrHelp)
}

func TestLoad_NoBrowserEnvironmentIsParsedAsABoolean(t *testing.T) {
	for value, wantOpen := range map[string]bool{
		"": true, "0": true, "false": true, "FALSE": true, "no": true, "off": true,
		"1": false, "true": false, "True": false, "yes": false, "on": false,
	} {
		cfg, err := config.Load(nil, env(map[string]string{"MAM_DATA_DIR": "/data", "MAM_NO_BROWSER": value}), io.Discard)
		require.NoError(t, err, value)
		assert.Equal(t, wantOpen, cfg.OpenBrowser, "MAM_NO_BROWSER=%q", value)
	}

	_, err := config.Load(nil, env(map[string]string{"MAM_DATA_DIR": "/data", "MAM_NO_BROWSER": "maybe"}), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MAM_NO_BROWSER")

	cfg, err := config.Load([]string{"--no-browser"}, env(map[string]string{"MAM_DATA_DIR": "/data", "MAM_NO_BROWSER": "maybe"}), io.Discard)
	require.NoError(t, err)
	assert.False(t, cfg.OpenBrowser, "an explicit flag wins over the environment")
}

func TestSettingsFiles_DefaultToTheDataDirAndAreNotPinned(t *testing.T) {
	cfg, err := config.Load(nil, env(map[string]string{"MAM_DATA_DIR": "/data"}), io.Discard)
	require.NoError(t, err)

	path, pinned := cfg.LLMSettingsFile()
	assert.Equal(t, filepath.Join("/data", "llm.json"), path)
	assert.False(t, pinned)
	path, pinned = cfg.ModelSettingsFile()
	assert.Equal(t, filepath.Join("/data", "models.json"), path)
	assert.False(t, pinned)
}

func TestSettingsFiles_AFlagOrEnvironmentPinsTheFile(t *testing.T) {
	cfg, err := config.Load([]string{"-llm-config", "/etc/mam/llm.json"},
		env(map[string]string{"MAM_DATA_DIR": "/data", "MAM_MODEL_CONFIG": "/etc/mam/models.json"}), io.Discard)
	require.NoError(t, err)

	path, pinned := cfg.LLMSettingsFile()
	assert.Equal(t, "/etc/mam/llm.json", path)
	assert.True(t, pinned, "--llm-config pins the file")
	path, pinned = cfg.ModelSettingsFile()
	assert.Equal(t, "/etc/mam/models.json", path)
	assert.True(t, pinned, "MAM_MODEL_CONFIG pins the file")
}
