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

	cfg, err := judge.LoadConfig(writeConfig(t, `{"face":{"warn_similarity":0.6,"fail_similarity":0.45}}`))
	require.NoError(t, err)
	assert.InDelta(t, 0.6, cfg.Face.WarnSimilarity, 1e-9, "thresholds are configuration")
	assert.InDelta(t, 0.45, cfg.Face.FailSimilarity, 1e-9)
	assert.Equal(t, def.Frames, cfg.Frames, "the rest keeps its default")
}

func TestLoadConfig_RejectsUnknownKeysAndUnusableNumbers(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":        `{"face":{"warn":0.6}}`,
		"fail above warn":    `{"face":{"warn_similarity":0.4,"fail_similarity":0.5}}`,
		"threshold zero":     `{"face":{"fail_similarity":0}}`,
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
