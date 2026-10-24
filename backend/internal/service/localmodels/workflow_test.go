package localmodels_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

func TestImportWorkflowAndTemplates(t *testing.T) {
	r := newRig(t, false, "")
	imp, err := r.svc.ImportWorkflow([]byte(`{"prompt":{"1":{"class_type":"CLIPTextEncode","inputs":{"text":"hi"}}}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"1":{"class_type":"CLIPTextEncode","inputs":{"text":"hi"}}}`, string(imp.Graph))
	require.Len(t, imp.Nodes, 1)
	assert.Equal(t, "CLIPTextEncode", imp.Nodes[0].Class)

	var invalid *localmodels.InvalidError
	require.ErrorAs(t, errOf(r.svc.ImportWorkflow([]byte(`{"nodes":[],"links":[]}`))), &invalid)
	assert.Contains(t, invalid.Message, "API Format")
	assert.Len(t, r.svc.Templates(), 11)

	// An imported graph with the user's assignments becomes a model.
	ctx := context.Background()
	require.NoError(t, r.svc.CreateEndpoint(ctx, comfy("http://127.0.0.1:8188")))
	require.NoError(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "mine", Model: localcfg.Model{
		Media: localcfg.MediaImage,
		Workflow: &localcfg.Workflow{
			Graph:       imp.Graph,
			Assignments: []localcfg.Assignment{{Param: localcfg.ParamPrompt, Node: "1", Input: "text"}},
			OutputNode:  "1",
		},
	}}))
}
