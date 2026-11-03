package settingstest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// A local model that has never run with these settings has no time in the estimate and says so: the field is "unknown", not a guess; a
// cloud model has none (Issue #1002).
func TestEstimateSaysTheTimeOfALocalModelIsUnknownWithoutHistory(t *testing.T) {
	r := newRig(t, gentesttest.WithNoReferenceModel())
	local := string(gentesttest.NoReferenceModel)
	args := map[string]any{"media": "image", "models": []string{local, "flux-2"}, "count": 2, "prompts": []map[string]any{{"prompt": "a red fox"}}}
	var raw map[string]any
	r.CallOK("estimate_generation_test", args, &raw)
	assert.Equal(t, []any{local}, raw["unknown_time_models"])
	assert.NotContains(t, raw, "total_seconds", "no model has a time")
	for _, l := range raw["breakdown"].([]any) {
		line := l.(map[string]any)
		if line["model"] == local {
			assert.Equal(t, "unknown", line["time_basis"])
			assert.NotEmpty(t, line["time_note"])
			assert.NotContains(t, line, "seconds_per_item")
		} else {
			assert.NotContains(t, line, "time_basis", "a vendor's queue decides how long a cloud model takes")
		}
	}
}
