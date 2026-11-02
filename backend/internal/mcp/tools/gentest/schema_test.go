package gentest_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A strength or a pose option that no chosen model can use is dropped without a warning: the tool says so up front.
func TestSchemaSaysWhereStrengthAndAvoidPoseHaveNoEffect(t *testing.T) {
	r := newRig(t)
	tools, err := r.Client.ListTools(t.Context(), nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		if tool.Name != "run_generation_test" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema struct {
			Props map[string]struct {
				Description string `json:"description"`
				Items       struct {
					Props map[string]struct {
						Description string `json:"description"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(raw, &schema))
		assert.Contains(t, schema.Props["avoid_reference_pose"].Description, "警告も出ない")
		assert.Contains(t, schema.Props["references"].Items.Props["strength"].Description, "無視され、警告も出ない")
		return
	}
	t.Fatal("run_generation_test is not registered")
}
