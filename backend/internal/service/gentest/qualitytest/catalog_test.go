package qualitytest_test

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// catalogWorkflows are the names of the workflows (the model names) the model manager's catalog creates for its image models.
func catalogWorkflows(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, m := range catalog.Default().Models {
		for _, w := range m.Workflows {
			out[w.Name] = true
		}
	}
	return out
}
