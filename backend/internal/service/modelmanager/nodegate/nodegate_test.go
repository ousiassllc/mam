package nodegate_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
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

// #1016: a model whose workflow needs ComfyUI-GGUF, which mam cannot install (its package gguf is not in the bundled Python), is neither installed nor
// chosen, and the reason is the one the section of the nodes shows (no "install it there" advice, which would lead nowhere). The screen's note, the
// refusal of the install (the MCP tool install_model shows it) and the generation test's list say the same words.
func TestAModelThatNeedsTheBlockedGGUFNodeIsNeitherInstalledNorChosen(t *testing.T) {
	mgr := nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})
	r := mmtest.New(t, mmtest.WithGGUFModel(mgr))
	r.InstallEngine()

	_, err := r.Svc.InstallModel(t.Context(), mmtest.GGUFModelID, nil)
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "導入できないため使えません")
	assert.Contains(t, invalid.Message, "ComfyUI-GGUF")
	assert.Contains(t, invalid.Message, "gguf", "the Python package that is missing")
	assert.Contains(t, invalid.Message, "pip", "mam runs no pip")
	assert.Contains(t, invalid.Message, nodegate.Section)
	assert.NotContains(t, invalid.Message, "導入してください")
	m, err := r.Svc.Model(mmtest.GGUFModelID)
	require.NoError(t, err)
	assert.Equal(t, []string{"comfyui-gguf"}, m.RequiresNodes)
	assert.Equal(t, modelmanager.ModelNotInstalled, m.State)
	assert.Empty(t, m.InstallJob, "no job was made")

	id := localcfg.ID(localcfg.KindComfyUI, modelmanager.BundledEndpoint, "wf-g")
	assert.Equal(t, invalid.Message[len("N は、"):], nodegate.ForModels(mgr, modelmanager.BundledEndpoint)(id, m.RequiresNodes), "the generation test lists it as unavailable with the same reason")
	assert.Equal(t, invalid.Message[len("N は、"):], nodegate.Reason(mgr, m.RequiresNodes), "the screen's note")
	assert.Empty(t, nodegate.ForModels(mgr, modelmanager.BundledEndpoint)(localcfg.ID(localcfg.KindComfyUI, "other", "wf-g"), m.RequiresNodes), "an external ComfyUI has its own nodes")
}

// Every GGUF model of the embedded catalog is refused by the same check while the node is blocked.
func TestTheGGUFModelsOfTheCatalogAreRefusedWhileTheNodeIsBlocked(t *testing.T) {
	mgr := nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})
	var found int
	for _, m := range catalog.Default().Models {
		if !strings.HasSuffix(m.Files[0].Name, ".gguf") {
			continue
		}
		found++
		assert.Equal(t, []string{"comfyui-gguf"}, nodegate.Required(m), m.ID)
		var invalid *modelmanager.InvalidError
		require.ErrorAs(t, nodegate.Check(mgr, m), &invalid, m.ID)
		assert.Contains(t, invalid.Message, "導入できないため使えません", m.ID)
	}
	assert.Equal(t, 2, found)
}
