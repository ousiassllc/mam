package workflows_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// Wherever the Video Model of a Shot is picked, the saved video workflows that are published are listed by their name
// and version, next to the built-in models; the one the Shot is pinned to is the selected choice.
func TestShotVideoModelPickListsSavedWorkflows(t *testing.T) {
	v1, v2 := modelreg.WorkflowID("0199d7a0-1111-7222-8333-444455556666", 1), modelreg.WorkflowID("0199d7a0-1111-7222-8333-444455556666", 2)
	modelreg.Set(nil, []string{v1, v2})
	modelreg.SetLabels(map[string]string{v1: "Clip (v1)", v2: "Clip (v2)"})
	t.Cleanup(func() { modelreg.Set(nil, nil); modelreg.SetLabels(nil) })

	locks := domain.ParameterLocks{domain.ParamVideoModel: {Mode: domain.LockLocked, Value: []byte(`"` + v1 + `"`)}}
	var b strings.Builder
	assert.NoError(t, molecules.LockToggle(organisms.LockToggleFor("s1", domain.ParamVideoModel, locks)).Render(context.Background(), &b))
	html := b.String()
	assert.Contains(t, html, `<option value="`+v1+`" selected>Clip (v1)</option>`)
	assert.Contains(t, html, `<option value="`+v2+`">Clip (v2)</option>`)
	assert.Contains(t, html, `<option value="kling-3.0">kling-3.0</option>`, "the built-in models are still there")
}
