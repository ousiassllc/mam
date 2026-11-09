package web_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/nodecard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/reason"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/runtimecard"
)

// The wtest static/src/components/layout/model-manager-grid.wtest.ts draws these files with the real Tailwind CSS at 639, 640, 1279 and 1280 px and checks
// that every list of cards of the model manager has the same columns (1, 2, 2, 3: modelcard.GridClass, #1102) and that the page does not scroll sideways.
// The models of the list are scroll-mm-page.html (scroll_fixture_test.go). They are what the components render:
// `MAM_UPDATE_FIXTURES=1 go test ./backend/internal/web -run TestModelManagerGridFixtures` writes them again.

// gridNodes is the five nodes of the allowlist as the page lists them, with the description each has in the catalog (the longest text a card used to carry).
func gridNodes() []nodecard.Node {
	var items []nodecard.Node
	for _, c := range nodecat.Default().Nodes {
		n := nodecard.Node{ID: c.ID, Name: c.Name, Description: c.Description, Size: "1 MiB", State: "not_installed", Models: []nodecard.Model{{Name: "モデル"}}}
		if c.InstallBlocked != "" {
			n.State, n.Blocked = "blocked", c.InstallBlocked
		}
		items = append(items, n)
	}
	return items
}

// gridRuntimes is a voice, a lip-sync and an LLM runtime that is installed and running, with the most buttons a card has.
func gridRuntimes() []runtimecard.Runtime {
	var items []runtimecard.Runtime
	for i, p := range []struct {
		name, purpose string
		icon          atoms.IconName
	}{{"GPT-SoVITS", "声", atoms.IconVoice}, {"MuseTalk", "口パク", atoms.IconLipSync}, {"llama.cpp", "文章（LLM）", atoms.IconPackage}} {
		items = append(items, runtimecard.Runtime{
			ID: "runtime-" + strconv.Itoa(i), Name: p.name, Purpose: p.purpose, Icon: p.icon, Need: "6.5 GiB",
			Supported: true, Writable: true, Installed: true, UpdateAvailable: true, State: "running",
		})
	}
	return items
}

// gridHFModels is the files added from Hugging Face.
func gridHFModels() []modelmgr.HFModel {
	var items []modelmgr.HFModel
	for i := 1; i <= 4; i++ {
		n := strconv.Itoa(i)
		items = append(items, modelmgr.HFModel{Path: "diffusion_models/added-" + n + ".safetensors", Name: "added-" + n + ".safetensors", Folder: "diffusion_models", Repo: "owner/repo-" + n, Size: "6.5 GiB"})
	}
	return items
}

// gridNodeDescriptions is the description of each node of the allowlist, which the page of the custom nodes must not show (#1103): the wtest reads the
// visible text of the page against them.
func gridNodeDescriptions(t *testing.T) templ.Component {
	t.Helper()
	var all []string
	for _, c := range nodecat.Default().Nodes {
		all = append(all, c.Description)
	}
	b, err := json.Marshal(all)
	require.NoError(t, err)
	return templ.Raw(string(b))
}

// gridNodeNeededModel is a model that waits for a custom node: its card has the link to the nodes' page in its body (mm-node-needed-link, #1110).
func gridNodeNeededModel() modelmgr.Catalog {
	return modelmgr.Catalog{Models: []modelmgr.Model{{
		ID: "model-node", Name: "ノードが要るモデル", KindLabel: "画像", Description: "試験用のモデルです。", Size: "6.5 GiB", Need: "6.5 GiB", VRAM: "12 GiB 以上",
		License: modelmgr.License{ID: "apache-2.0", Version: "1", Name: "Apache-2.0", URL: "https://example.com/license", Commercial: "可"},
		State:   "not_installed", NeedsNodes: "追加ノード「試験用ノード」が必要です", Files: []modelmgr.File{{Path: "checkpoints/model-node.safetensors", Size: "6.5 GiB"}},
		ModelIDs: []string{"model-node"}, WorkflowCheck: "ワークフロー確認済み",
	}}}
}

// gridReasonPopup is the popup the reason button of gridNodeNeededModel opens (#1118): the longest reason of the allowlist (ComfyUI-GGUF, which mam does not install) and
// the link to the nodes.
func gridReasonPopup() reason.Popup {
	var long string
	for _, c := range nodecat.Default().Nodes {
		if c.ID == "comfyui-gguf" {
			long = c.InstallBlocked
		}
	}
	return reason.Popup{
		Title: "「ノードが要るモデル」を導入できない理由と対処", Lead: "このモデルを使うには、追加ノードが要ります。",
		Items:  []reason.Item{{Name: "追加ノード「ComfyUI-GGUF」", Text: "今の mam では導入できません。" + long}},
		Remedy: "このモデルが必要とする追加ノードは、今の mam では導入できません。mam は Python の依存を自動で入れません。導入できるようになるまで、このモデルは導入できません。",
		Link:   &reason.Link{Href: "/model-manager/custom-nodes#mm-nodes", Label: "追加ノードの画面へ"},
	}
}

func TestModelManagerGridFixtures_AreWhatTheComponentsRender(t *testing.T) {
	checkFixtures(t, map[string]templ.Component{
		"layout-mm-grid-node-descriptions.json": gridNodeDescriptions(t),
		"layout-mm-card-node-needed.html":       modelmgr.Body(mmcat.ImageVideo, modelmgr.Data{Catalog: gridNodeNeededModel()}),
		"mm-reason-popup.html":                  reason.Body(gridReasonPopup()),
		"mm-reason-host.html":                   modal.Host(),
		"layout-mm-grid-custom-nodes.html":      modelmgr.Body(mmcat.CustomNodes, modelmgr.Data{Nodes: nodecard.NodesSection(nodecard.Section{Items: gridNodes()})}),
		"layout-mm-grid-local-runtimes.html":    modelmgr.Body(mmcat.LocalRuntimes, modelmgr.Data{Runtimes: runtimecard.RuntimesSection(runtimecard.Runtimes{Items: gridRuntimes()})}),
		"layout-mm-grid-hf-external.html":       modelmgr.Body(mmcat.HFExternal, modelmgr.Data{HFImport: modelmgr.HFImport{Kind: "text-to-image", Models: modelmgr.HFModelList{Models: gridHFModels()}}}),
	})
}
