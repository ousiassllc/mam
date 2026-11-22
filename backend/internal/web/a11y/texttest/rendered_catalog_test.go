package texttest_test

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/web/a11y/uisrc"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// localEditorScreens is the first step of a ComfyUI model (the choice of a built-in workflow) over the real catalog of templates: the description of each is a
// sentence or more of its JSON, so the size and the colour it is set in are the ones of a text to read (T3, C6), whatever the length of the file it came from.
func localEditorScreens(t *testing.T) map[string]string {
	t.Helper()
	var d local.Data
	for _, tpl := range localcfg.Templates() {
		d.Templates = append(d.Templates, local.TemplateOption{ID: tpl.ID, Label: tpl.Label, Media: string(tpl.Media), Description: tpl.Description})
	}
	d.Editor.Start = &local.ModelStart{Endpoint: "gpu-box", Media: local.MediaImage, TemplateID: d.Templates[0].ID}
	return map[string]string{"settings/local-models/endpoint/models/new": uisrc.Render(t, local.EditorBody(d))}
}

// workflowScreens is the workflows screen with a workflow open in its editor: the sections under the h1 (the editor, the two imports) are what the heading levels of the
// screen are checked on (T7).
func workflowScreens(t *testing.T) map[string]string {
	t.Helper()
	d := workflows.Data{Editor: &workflows.Editor{ID: "wf1", Name: "portrait", Kind: workflows.KindImage, Version: 1}}
	return map[string]string{"workflows": uisrc.Render(t, pages.WorkflowsPage("tok", d))}
}
