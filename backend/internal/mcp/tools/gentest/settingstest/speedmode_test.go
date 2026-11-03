package settingstest_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// speed_mode (Issue #930: Wan 2.2 14B + LightX2V, 4 steps or the usual 20) is an argument of both estimate and run, it is explained in the
// schema, and a model that has no such mode gets the warning and still runs.
func TestSpeedModeIsInTheSchemaOfEstimateAndRun(t *testing.T) {
	r := newRig(t)
	tools, err := r.Client.ListTools(t.Context(), nil)
	require.NoError(t, err)
	seen := 0
	for _, tool := range tools.Tools {
		if tool.Name != "estimate_generation_test" && tool.Name != "run_generation_test" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema struct {
			Props map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(raw, &schema))
		assert.Contains(t, schema.Props["speed_mode"].Description, "video.speed_modes", tool.Name)
		assert.Contains(t, schema.Props["speed_mode"].Description, "speed_mode_unsupported", tool.Name)
		seen++
	}
	assert.Equal(t, 2, seen)
}

func TestAModelWithoutTheSpeedModeGetsTheWarning(t *testing.T) {
	r := newRig(t)
	args := map[string]any{"media": "video", "models": []string{"kling-3.0"}, "prompts": []map[string]any{{"prompt": "x"}}, "speed_mode": "standard"}
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	var warned bool
	for _, w := range est.Warnings {
		warned = warned || (w.Code == gentest.WarnSpeedUnsupported && w.Model == "kling-3.0")
	}
	assert.True(t, warned, "%+v", est.Warnings)

	image := map[string]any{"media": "image", "models": []string{"flux-2"}, "prompts": []map[string]any{{"prompt": "x"}}, "speed_mode": "fast"}
	assert.Contains(t, r.CallError("estimate_generation_test", image), "validation", "speed modes are for video")
}
