package workflowstest_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// rig is the workflows service over a temporary database and a fake ComfyUI registered as the bundled endpoint.
type rig struct {
	svc     *workflows.Service
	repo    *comfywf.Repository
	comfy   *comfyuitest.Fake
	adapter *modeladapter.Adapter
	models  *modelsettings.Service
	local   *localmodels.Service
	runs    string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	db := testdb.Open(t)
	repo := comfywf.New(db)
	t.Cleanup(func() {
		localcfg.PublishWorkflows(nil)
		localbackends.Publish(modeladapter.DefaultConfig())
	})
	require.NoError(t, workflows.Publish(ctx, repo, nil))

	path := filepath.Join(t.TempDir(), "models.json")
	keys := credentialtest.New()
	cfg, err := configcheck.Load("")
	require.NoError(t, err)
	opts := localbackends.Options{StagingDir: t.TempDir(), PollEvery: time.Millisecond}
	adapter := modeladapter.New(cfg, localbackends.Build(cfg, keys, opts))
	localbackends.Publish(cfg)
	local := localmodels.New(localmodels.Deps{Path: path, Keys: keys, Adapter: adapter, Backends: opts})
	models := modelsettings.New(path, false)
	models.SetOnChange(func() { require.NoError(t, local.Reload()) })

	comfy := comfyuitest.New(t)
	require.NoError(t, local.CreateEndpoint(ctx, localmodels.EndpointInput{Name: workflows.BundledEndpoint, Kind: localcfg.KindComfyUI, BaseURL: comfy.URL()}))

	runs := t.TempDir()
	svc := workflows.New(workflows.Deps{Repo: repo, Models: local, Adapter: adapter, Keys: keys, StagingDir: t.TempDir(), RunsDir: runs})
	return &rig{svc: svc, repo: repo, comfy: comfy, adapter: adapter, models: models, local: local, runs: runs}
}

// template is a built-in workflow as the API JSON and the assignment the service stores.
func template(t *testing.T, id string) (graph []byte, spec localcfg.WorkflowSpec) {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	w := tpl.Model.Workflow
	spec = localcfg.WorkflowSpec{Assignments: w.Assignments, OutputNode: w.OutputNode, FPS: w.FPS, FramesStep: w.FramesStep, SizeMultiple: w.SizeMultiple, Video: tpl.Model.Video}
	return w.Graph, spec
}

// editorFormat is a minimal ComfyUI editor-format workflow with the id the runs of the history carry.
func editorFormat(id string) string {
	raw, _ := json.Marshal(map[string]any{"id": id, "nodes": []any{}, "links": []any{}})
	return string(raw)
}
