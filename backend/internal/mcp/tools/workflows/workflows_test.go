package workflows_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/director"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/workflows"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
)

type rig struct {
	*shottest.Fixture
	comfy *comfyuitest.Fake
}

// newRig serves the workflow tools over a fake ComfyUI registered as the bundled endpoint.
func newRig(t *testing.T, withService bool) *rig {
	t.Helper()
	comfy := comfyuitest.New(t)
	keys := credentialtest.New()
	cfg, err := configcheck.Load("")
	require.NoError(t, err)
	opts := localbackends.Options{StagingDir: t.TempDir(), PollEvery: time.Millisecond}
	adapter := modeladapter.New(cfg, localbackends.Build(cfg, keys, opts))
	localbackends.Publish(cfg)
	local := localmodels.New(localmodels.Deps{Path: filepath.Join(t.TempDir(), "models.json"), Keys: keys, Adapter: adapter, Backends: opts})
	require.NoError(t, local.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: workflowsvc.BundledEndpoint, Kind: localcfg.KindComfyUI, BaseURL: comfy.URL()}))
	t.Cleanup(func() {
		localcfg.PublishWorkflows(nil)
		localbackends.Publish(modeladapter.DefaultConfig())
	})
	f := shottest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		if withService {
			env.Services.Workflows = workflowsvc.New(workflowsvc.Deps{
				Repo: comfywf.New(env.DB), Models: local, Adapter: adapter, Keys: keys, StagingDir: t.TempDir(), RunsDir: t.TempDir(),
			})
		}
		jobs.Register(reg, env.Services)
		director.Register(reg, env.Services)
		workflows.Register(reg, env.Services)
	}, shottest.Options{Models: adapter})
	return &rig{Fixture: f, comfy: comfy}
}

// template is a built-in workflow as the JSON text and settings the tools take.
func template(t *testing.T, id string) (string, workflows.Settings) {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	w := tpl.Model.Workflow
	spec := localcfg.WorkflowSpec{Assignments: w.Assignments, OutputNode: w.OutputNode, FPS: w.FPS, FramesStep: w.FramesStep, SizeMultiple: w.SizeMultiple, Video: tpl.Model.Video}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	var s workflows.Settings
	require.NoError(t, json.Unmarshal(raw, &s))
	return string(w.Graph), s
}

func (r *rig) importWorkflow(t *testing.T, name, kind, template_ string) workflows.Workflow {
	t.Helper()
	graph, settings := template(t, template_)
	var w workflows.Workflow
	r.CallOK("import_workflow_json", map[string]any{"name": name, "kind": kind, "api_json": graph, "settings": settings}, &w)
	return w
}

func TestImportListGetRenameUpdateAndDelete(t *testing.T) {
	r := newRig(t, true)
	w := r.importWorkflow(t, "Stills", "image", "txt2img-sdxl")
	assert.Equal(t, 1, w.Version)
	assert.True(t, w.Ready, "%v", w.Problems)
	var saved workflows.SavedOutput
	graph0, settings0 := template(t, "txt2img-sdxl")
	r.CallOK("import_workflow_json", map[string]any{"name": "Other", "kind": "image", "api_json": graph0, "settings": settings0}, &saved)
	assert.True(t, saved.Check.Reachable)
	assert.NotEmpty(t, saved.Check.Warnings, "the save is checked against the connected ComfyUI and warns about what it lacks")
	r.CallOK("delete_workflow", map[string]any{"workflow_id": saved.ID}, nil)
	assert.Equal(t, "comfy:"+w.ID+"@v1", w.ModelID)

	var list workflows.ListOutput
	r.CallOK("list_workflows", map[string]any{}, &list)
	require.Len(t, list.Workflows, 1)
	assert.Equal(t, w.ID, list.Workflows[0].ID)

	var got workflows.GetOutput
	r.CallOK("get_workflow", map[string]any{"workflow_id": w.ID}, &got)
	assert.Empty(t, got.Graph, "the graph only when asked")
	assert.NotEmpty(t, got.Nodes)
	assert.NotEmpty(t, got.Settings.Assignments)
	assert.Equal(t, []workflows.VersionInfo{{Version: 1, ModelID: w.ModelID, Current: true}}, got.Versions)
	r.CallOK("get_workflow", map[string]any{"workflow_id": w.ID, "include_graph": true}, &got)
	assert.NotEmpty(t, got.Graph)

	var renamed workflows.Workflow
	r.CallOK("rename_workflow", map[string]any{"workflow_id": w.ID, "name": "Hero stills"}, &renamed)
	assert.Equal(t, "Hero stills", renamed.Name)
	assert.Equal(t, 1, renamed.Version, "a rename makes no version")

	settings := got.Settings
	settings.SizeMultiple = 64
	var updated workflows.Workflow
	r.CallOK("update_workflow_assignment", map[string]any{"workflow_id": w.ID, "assignments": settings.Assignments, "output_node": settings.OutputNode, "size_multiple": 64}, &updated)
	assert.Equal(t, 2, updated.Version, "an assignment change makes a version")
	r.CallOK("get_workflow", map[string]any{"workflow_id": w.ID}, &got)
	assert.Len(t, got.Versions, 2)
	assert.Equal(t, updated.ModelID, got.Versions[0].ModelID)

	assert.Contains(t, r.CallError("update_workflow_assignment", map[string]any{"workflow_id": w.ID, "assignments": []any{}, "output_node": "404"}), "validation")
	assert.Contains(t, r.CallError("import_workflow_json", map[string]any{"name": "hero STILLS", "kind": "image", "api_json": "{}"}), "validation", "an empty graph")
	graph, _ := template(t, "txt2img-sdxl")
	assert.Contains(t, r.CallError("import_workflow_json", map[string]any{"name": "hero stills", "kind": "image", "api_json": graph}), "conflict", "the name is taken")
	assert.Contains(t, r.CallError("import_workflow_json", map[string]any{"name": "x", "kind": "image", "api_json": `{"nodes":[],"links":[]}`}), "validation", "the editor format")
	assert.Contains(t, r.CallError("get_workflow", map[string]any{"workflow_id": "missing"}), "not_found")
	assert.Contains(t, r.CallError("rename_workflow", map[string]any{"workflow_id": "missing", "name": "x"}), "not_found")

	var deleted workflows.DeleteOutput
	r.CallOK("delete_workflow", map[string]any{"workflow_id": w.ID}, &deleted)
	assert.True(t, deleted.Deleted)
	r.CallOK("list_workflows", map[string]any{}, &list)
	assert.Empty(t, list.Workflows)
	assert.Contains(t, r.CallError("delete_workflow", map[string]any{"workflow_id": w.ID}), "not_found")
}

func TestToolsAreUnavailableWithoutTheService(t *testing.T) {
	r := newRig(t, false)
	assert.Contains(t, r.CallError("list_workflows", map[string]any{}), "unavailable")
	assert.Contains(t, r.CallError("run_workflow", map[string]any{"workflow_id": "x", "prompt": "y"}), "unavailable")
}

func TestCheckWorkflowWarnsAndListsTheServersWorkflows(t *testing.T) {
	r := newRig(t, true)
	w := r.importWorkflow(t, "Stills", "image", "txt2img-sdxl")
	r.comfy.ObjectInfo = json.RawMessage(`{"CLIPTextEncode":{"input":{"required":{"text":["STRING",{}]}}}}`)
	r.comfy.Models = map[string][]string{"checkpoints": {}}

	var res workflows.CheckOutput
	r.CallOK("check_workflow", map[string]any{"workflow_id": w.ID}, &res)
	assert.True(t, res.Reachable)
	assert.NotEmpty(t, res.Warnings, "the nodes the server lacks")
	r.comfy.Down = true
	r.CallOK("check_workflow", map[string]any{"workflow_id": w.ID}, &res)
	assert.False(t, res.Reachable)
	assert.NotEmpty(t, res.Reason)
	assert.Empty(t, res.Warnings)
	r.comfy.Down = false

	graph, _ := template(t, "txt2img-sdxl")
	extra, _ := json.Marshal(map[string]any{"extra_pnginfo": map[string]any{"workflow": map[string]any{"id": "flow-a", "nodes": []any{}}}})
	run, _ := json.Marshal([]json.RawMessage{json.RawMessage(`1`), json.RawMessage(`"p1"`), json.RawMessage(graph), extra, json.RawMessage(`["9"]`)})
	r.comfy.UserData = map[string]string{"hero.json": `{"id":"flow-a","nodes":[],"links":[]}`}
	r.comfy.HistoryList = []json.RawMessage{run}

	var remote workflows.ListRemoteOutput
	r.CallOK("list_comfyui_workflows", map[string]any{}, &remote)
	require.Len(t, remote.Workflows, 1)
	assert.Equal(t, "hero.json", remote.Workflows[0].Path)
	var imported workflows.Workflow
	r.CallOK("import_workflow_from_comfyui", map[string]any{"path": "hero.json"}, &imported)
	assert.Equal(t, "hero", imported.Name)
	assert.True(t, imported.HasEditor)
	assert.Contains(t, r.CallError("import_workflow_from_comfyui", map[string]any{"path": "../x.json"}), "validation")
}
