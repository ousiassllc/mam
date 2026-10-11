package judge_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "judge.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoadConfig_DefaultsAreValidAndTheFileOverridesPartsOfThem(t *testing.T) {
	require.NoError(t, judge.DefaultConfig().Validate())
	def, err := judge.LoadConfig("")
	require.NoError(t, err)
	assert.Equal(t, judge.DefaultConfig(), def)

	cfg, err := judge.LoadConfig(writeConfig(t, `{"face":{"calibration_warn_scale":0.9,"calibration_fail_scale":0.5}}`))
	require.NoError(t, err)
	assert.InDelta(t, 0.9, cfg.Face.CalibrationWarnScale, 1e-9, "scales are configuration")
	assert.InDelta(t, 0.5, cfg.Face.CalibrationFailScale, 1e-9)
	assert.Equal(t, def.Frames, cfg.Frames, "the rest keeps its default")
}

func TestLoadConfig_RejectsUnknownKeysAndUnusableNumbers(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":        `{"face":{"warn":0.6}}`,
		"fixed thresholds":   `{"face":{"warn_similarity":0.5,"fail_similarity":0.363}}`,
		"calibration order":  `{"face":{"calibration_warn_scale":0.5,"calibration_fail_scale":0.7}}`,
		"undetected too big": `{"face":{"max_undetected_ratio":1}}`,
		"no frames":          `{"frames":{"review_frames":0}}`,
		"not json":           `{`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := judge.LoadConfig(writeConfig(t, body))
			assert.Error(t, err)
		})
	}
	_, err := judge.LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
	assert.Error(t, err)
}
