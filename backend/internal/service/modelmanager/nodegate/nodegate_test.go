package nodegate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/nodegate"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes/nodetest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

func TestRequiredIsWhatTheTemplatesDeclare(t *testing.T) {
	ip := catalog.Model{Workflows: []catalog.Workflow{{Template: "txt2img-sdxl-ipadapter"}, {Template: "txt2img-sdxl"}}}
	assert.Equal(t, []string{"ipadapter-plus"}, nodegate.Required(ip))
	cn := catalog.Model{Workflows: []catalog.Workflow{{Template: "txt2img-sdxl-controlnet"}}}
	assert.Empty(t, nodegate.Required(cn), "ControlNet of the core nodes needs no custom node")
}

// A model whose workflow needs a custom node is not installed while the node is missing: the refusal names the node and says where to install it,
// and no job is made. Once the node is installed the same install goes through, and the generation test lists the model only then.
func TestAModelThatNeedsANodeInstallsOnlyAfterTheNode(t *testing.T) {
	nodes := nodetest.NewFor(t, "ipadapter-plus")
	r := mmtest.New(t, mmtest.WithNodeModel(nodes.Nodes))
	r.InstallEngine()

	_, err := r.Svc.InstallModel(t.Context(), mmtest.NodeModelID, nil)
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "追加ノード「Pack」")
	assert.Contains(t, invalid.Message, nodegate.Section)
	m, err := r.Svc.Model(mmtest.NodeModelID)
	require.NoError(t, err)
	assert.Equal(t, []string{"ipadapter-plus"}, m.RequiresNodes)
	assert.Equal(t, modelmanager.ModelNotInstalled, m.State)
	assert.Empty(t, m.InstallJob, "no job was made")

	gate := nodegate.ForModels(nodes.Nodes, modelmanager.BundledEndpoint)
	id := localcfg.ID(localcfg.KindComfyUI, modelmanager.BundledEndpoint, "wf-n")
	assert.Contains(t, gate(id, m.RequiresNodes), "追加ノード「Pack」")
	assert.Empty(t, gate(localcfg.ID(localcfg.KindComfyUI, "other", "wf-n"), m.RequiresNodes), "an external ComfyUI has its own nodes")

	ref, err := nodes.Svc.Install(t.Context(), "ipadapter-plus", nodes.Accept())
	require.NoError(t, err)
	require.Equal(t, domain.JobSucceeded, nodes.Wait(ref.JobID).State)
	assert.Empty(t, gate(id, m.RequiresNodes), "the node is installed: the model can be chosen")

	st := r.InstallModel(mmtest.NodeModelID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)

	// The generation test lists the model while the node is there; with the node removed it is listed as not available, with the reason, and a
	// batch for it is refused (request.go: an unavailable model is not run).
	cat := modelcatalog.Catalog{Adapter: r.Adapter, MissingNodes: gate}
	wf, ok := cat.Model("image", id)
	require.True(t, ok)
	assert.True(t, wf.Available, wf.UnavailableMessage)

	require.NoError(t, nodes.Svc.Remove(t.Context(), "ipadapter-plus"))
	wf, ok = cat.Model("image", id)
	require.True(t, ok)
	assert.False(t, wf.Available)
	assert.Equal(t, modelcatalog.ExcludedMissingNode, wf.Unavailable)
	assert.Contains(t, wf.UnavailableMessage, "追加ノード「Pack」")
}
