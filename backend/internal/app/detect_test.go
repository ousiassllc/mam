package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/app"
)

func TestRunDetectIgnoresOrdinaryArguments(t *testing.T) {
	var out bytes.Buffer
	handled, err := app.RunDetect(context.Background(), []string{"--port", "9000", "--no-browser", "--model-config", "detect-gpu"}, &out)
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Empty(t, out.String())
}

func TestRunDetectPrintsTheGPUReportAsJSON(t *testing.T) {
	var out bytes.Buffer
	handled, err := app.RunDetect(context.Background(), []string{"--detect-gpu"}, &out)
	require.NoError(t, err)
	assert.True(t, handled)
	var report struct {
		OS             string         `json:"os"`
		Adapters       []any          `json:"adapters"`
		Recommendation map[string]any `json:"recommendation"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &report), out.String())
	assert.NotEmpty(t, report.OS)
	assert.NotNil(t, report.Adapters)
	assert.Contains(t, report.Recommendation, "reason")
}

func TestRunDetectPrintsTheComfyUIReport(t *testing.T) {
	var out bytes.Buffer
	handled, err := app.RunDetect(context.Background(), []string{"-detect-comfyui"}, &out)
	require.NoError(t, err)
	assert.True(t, handled)
	var report map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out.Bytes(), &report), out.String())
	assert.Contains(t, report, "found")
	assert.Contains(t, report, "desktop_config")
	assert.Contains(t, report, "recommended_url")
}

func TestRunDetectWithBothOptionsAndAnOutputFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "detect.json")
	var out bytes.Buffer
	handled, err := app.RunDetect(context.Background(), []string{"--detect-gpu", "--detect-comfyui", "--detect-output", file}, &out)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Empty(t, out.String(), "the report went to the file")
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	var report map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Contains(t, report, "gpu")
	assert.Contains(t, report, "comfyui")

	file2 := filepath.Join(t.TempDir(), "d2.json")
	_, err = app.RunDetect(context.Background(), []string{"--detect-gpu", "--detect-output=" + file2}, &out)
	require.NoError(t, err)
	assert.FileExists(t, file2)
}

// The options are part of the command line: it validates, and --help lists them.
func TestDetectOptionsAreValidArguments(t *testing.T) {
	require.NoError(t, app.ValidateArgs([]string{"--detect-gpu", "--detect-comfyui", "--detect-output", "x.json"}, func(string) string { return "" }))
	err := app.ValidateArgs([]string{"--help"}, func(string) string { return "" })
	var usage *app.UsageError
	require.ErrorAs(t, err, &usage)
	assert.Contains(t, usage.Text, "detect-gpu")
	assert.Contains(t, usage.Text, "detect-comfyui")
}
