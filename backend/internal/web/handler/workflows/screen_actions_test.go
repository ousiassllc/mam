package workflows_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func (s *screen) workflowID(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`data-workflow="([^"]+)"`).FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return m[1]
}

func TestImportPasteOrUploadSavesAndListsTheWorkflow(t *testing.T) {
	s := newScreen(t)
	graph := graphOf(t, "txt2img-sdxl")

	rec := s.upload(paths.WorkflowsPart("import"), map[string]string{"name": "Stills", "kind": "image", "api_json": graph}, "", true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="workflows-notice"`)
	assert.Contains(t, body, "Stills")
	assert.Contains(t, body, "comfy:")
	assert.Contains(t, body, `data-testid="workflow-version">v1`)
	assert.Contains(t, body, `data-testid="workflow-warnings"`, "the fake ComfyUI lacks the nodes: the save is checked and warns, and still saves")
	assert.Contains(t, s.get(paths.Workflows()).Body.String(), "Stills", "it is on the screen after a reload")

	dup := s.upload(paths.WorkflowsPart("import"), map[string]string{"name": "stills", "kind": "image"}, graph, true)
	assert.Equal(t, http.StatusConflict, dup.Code)
	assert.Contains(t, dup.Body.String(), "その名前のワークフローはすでにあります")

	editor := s.upload(paths.WorkflowsPart("import"), map[string]string{"name": "x", "kind": "image", "api_json": `{"nodes":[],"links":[]}`}, "", true)
	assert.Equal(t, http.StatusUnprocessableEntity, editor.Code)
	empty := s.upload(paths.WorkflowsPart("import"), map[string]string{"name": "y", "kind": "image"}, "", true)
	assert.Equal(t, http.StatusUnprocessableEntity, empty.Code)
	assert.Contains(t, empty.Body.String(), "API 形式の JSON")
}

func TestEditRenameDeleteAndCheck(t *testing.T) {
	s := newScreen(t)
	rec := s.upload(paths.WorkflowsPart("import"), map[string]string{"name": "Stills", "kind": "image", "api_json": graphOf(t, "txt2img-sdxl")}, "", true)
	require.Equal(t, http.StatusOK, rec.Code)
	id := s.workflowID(t, rec.Body.String())

	edit := s.get(paths.Workflow(id, "edit"))
	require.Equal(t, http.StatusOK, edit.Code)
	assert.Contains(t, edit.Body.String(), `data-testid="workflow-editor-form"`)
	assert.Contains(t, edit.Body.String(), `data-testid="workflow-assignments"`)
	assert.Equal(t, http.StatusNotFound, s.get(paths.Workflow("missing", "edit")).Code)

	check := s.form(http.MethodPost, paths.Workflow(id, "check"), nil, true)
	require.Equal(t, http.StatusOK, check.Code)
	assert.Contains(t, check.Body.String(), `data-testid="workflow-warnings"`, "the fake ComfyUI has none of the nodes")

	s.comfy.Down = true
	down := s.form(http.MethodPost, paths.Workflow(id, "check"), nil, true)
	assert.Contains(t, down.Body.String(), `data-testid="workflow-unreachable"`)
	s.comfy.Down = false

	del := s.form(http.MethodDelete, paths.Workflow(id), nil, true)
	require.Equal(t, http.StatusOK, del.Code)
	assert.Contains(t, del.Body.String(), `data-testid="workflows-empty"`)
	assert.Equal(t, http.StatusNotFound, s.form(http.MethodDelete, paths.Workflow(id), nil, true).Code)
}

func TestImportFromComfyUIListsAndImportsTheServersWorkflows(t *testing.T) {
	s := newScreen(t)
	s.comfy.UserData = map[string]string{"hero.json": `{"id":"a","nodes":[],"links":[]}`}

	list := s.get(paths.WorkflowsPart("remote"))
	require.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), `data-testid="workflow-remote-import-form"`)
	assert.Contains(t, list.Body.String(), "hero.json")

	// No run of it in the history: the API format is missing, which the screen says instead of saving half of it.
	rec := s.form(http.MethodPost, paths.WorkflowsPart("import", "comfyui"), url.Values{"remote_path": {"hero.json"}, "kind": {"image"}}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="workflows-error"`)
}

func TestEveryChangeNeedsTheCSRFToken(t *testing.T) {
	s := newScreen(t)
	rec := s.upload(paths.WorkflowsPart("import"), map[string]string{"name": "Stills", "kind": "image", "api_json": graphOf(t, "txt2img-sdxl")}, "", false)
	require.Equal(t, http.StatusOK, s.upload(paths.WorkflowsPart("import"), map[string]string{"name": "Stills", "kind": "image", "api_json": graphOf(t, "txt2img-sdxl")}, "", true).Code)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	list, err := s.svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, list, 1, "only the request with the token saved anything")
	id := list[0].ID
	for method, path := range map[string]string{
		http.MethodPut: paths.Workflow(id), http.MethodDelete: paths.Workflow(id),
	} {
		assert.Equal(t, http.StatusForbidden, s.form(method, path, nil, false).Code, method+" "+path)
	}
	for _, path := range []string{paths.Workflow(id, "check"), paths.Workflow(id, "open"), paths.NodesStart(), paths.WorkflowsPart("import", "comfyui")} {
		assert.Equal(t, http.StatusForbidden, s.form(http.MethodPost, path, nil, false).Code, path)
	}
	list, _ = s.svc.List(t.Context())
	assert.Len(t, list, 1, "nothing was deleted")
}
