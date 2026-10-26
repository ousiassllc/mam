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
	"github.com/ousiassllc/mam/backend/internal/web/organisms/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
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

// A ComfyUI that refuses to be framed is not drawn: the page says why, how to allow it, and keeps the link that opens
// it in the browser.
func TestNodeScreenOfARejectingComfyUIHasNoFrameAndTheWayOut(t *testing.T) {
	var b strings.Builder
	d := workflows.NodeData{
		Endpoint: "gpu", Endpoints: []string{"bundled-comfyui", "gpu"}, State: workflows.NodeRejected,
		Reason:  "この ComfyUI は、別のサイト（mam）の中に表示されることを拒否しています（403）。--enable-cors-header http://mam.example:8787 を付けるか、ブラウザで開いてください",
		OpenURL: "http://100.64.0.9:8188/", ModelManagerURL: "/model-manager",
	}
	assert.NoError(t, pages.NodesPage("tok", d).Render(context.Background(), &b))
	html := b.String()
	assert.NotContains(t, html, "<mam-comfy-frame")
	assert.Contains(t, html, `data-state="rejected"`)
	assert.Contains(t, html, "--enable-cors-header http://mam.example:8787")
	assert.Contains(t, html, `href="http://100.64.0.9:8188/"`)
	assert.Contains(t, html, `data-testid="nodes-open"`)
}
