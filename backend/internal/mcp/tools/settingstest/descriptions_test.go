// Package settingstest tests what the settings tools tell an MCP client in their schema descriptions (kept out of settings, which is at the line limit).
package settingstest_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
)

// #1113: an unusable model no longer always means a refused generation. The descriptions the client reads say what happens: a default assignment is
// replaced by a matching usable model; a model the user chose or pinned is refused; `routes` has the real handling.
func TestModelsSection_DescribesWhatHappensToAnAssignmentOfAnUnusableModel(t *testing.T) {
	available, _ := reflect.TypeFor[settings.ModelAvailability]().FieldByName("Available")
	unusable, _ := reflect.TypeFor[settings.ModelsSection]().FieldByName("Unusable")
	for _, f := range []reflect.StructField{available, unusable} {
		desc := f.Tag.Get("jsonschema")
		for _, want := range []string{"条件に合う使えるモデルに替わり", "断られる", "routes"} {
			assert.Contains(t, desc, want, f.Name)
		}
	}
}
