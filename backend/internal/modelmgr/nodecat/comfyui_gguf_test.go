package nodecat_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
)

// #1016: ComfyUI-GGUF is on the allowlist with its pin and is not installable: it imports the Python package gguf when it loads, the bundled
// environment (ComfyUI's portable build, the requirements.txt of the pinned release) does not have it, and mam runs no pip (custom-nodes.md §5.1, §5.5).
func TestComfyUIGGUFIsAllowlistedButNotInstallable(t *testing.T) {
	c := nodecat.Default()
	n, ok := c.Node("comfyui-gguf")
	require.True(t, ok)

	// The fork that knows the architectures of Qwen-Image 2.1 and Krea 2 (the upstream city96/ComfyUI-GGUF at 6ea2651e does not).
	assert.Equal(t, "https://github.com/leejet/ComfyUI-GGUF", n.Repo)
	assert.Equal(t, "373048b8403a7820620065210a691263d4da0a61", n.Commit)
	assert.Equal(t, "692c367072b6c1ec87ac9f3410c8e2563dfb8c0bb7f4abead09a8d6515c67169", n.ArchiveSHA256)
	assert.Equal(t, "ComfyUI-GGUF-373048b8403a7820620065210a691263d4da0a61", n.ArchiveRoot())
	assert.Equal(t, "ComfyUI-GGUF", n.Folder)
	assert.Contains(t, n.NodeClasses, "UnetLoaderGGUF")

	assert.Contains(t, n.PythonRequires, "gguf")
	assert.False(t, c.Python.Has("gguf"), "the bundled environment is not extended (§5.1)")
	assert.False(t, n.Installable())
	assert.Contains(t, n.InstallBlocked, "gguf")
	assert.Contains(t, n.InstallBlocked, "pip")

	assert.Equal(t, modellicense.Yes, n.License.Commercial, "Apache-2.0 for the code; the weights it loads have their own license")
	assert.Contains(t, n.Review, "ネットワーク通信")
	assert.Contains(t, n.Review, "qwen_image21", "the architecture of the Qwen-Image-2.1-Uncensored GGUF")
}

// The GGUF models of the catalog (#1016) name the blocked node in requires_nodes (through the template of their workflow): they are listed, and
// the install and the choice wait for the node (service/modelmanager/nodegate). A template that loads a GGUF without naming the node is refused.
func TestGGUFModelsRequireTheBlockedNode(t *testing.T) {
	var ids []string
	for _, m := range catalog.Default().Models {
		for _, f := range m.Files {
			if !strings.HasSuffix(f.Name, ".gguf") {
				continue
			}
			ids = append(ids, m.ID)
			assert.Equal(t, "diffusion_models", f.Folder, "the folder UnetLoaderGGUF reads (unet_gguf: diffusion_models or unet)")
			require.Len(t, m.Workflows, 1, m.ID)
			tpl, ok := localcfg.TemplateByID(m.Workflows[0].Template)
			require.True(t, ok)
			assert.Equal(t, []string{"comfyui-gguf"}, tpl.Model.Workflow.RequiresNodes, m.ID)
		}
	}
	assert.Equal(t, []string{"qwen-image-21-uncensored-gguf-q4-k-m", "qwen-image-21-uncensored-gguf-q8-0"}, ids,
		"LUSTIFY v10 Krea 2 GGUF is not on Hugging Face (custom-nodes.md §5.5)")
	assert.Empty(t, joined(nodecat.Validate(nodecat.Default(), catalog.Default(), localcfg.TemplateByID)))

	graph := `{"1": {"class_type": "UnetLoaderGGUF", "inputs": {"unet_name": "m.gguf"}}}`
	unnamed := func(id string) (localcfg.Template, bool) {
		return localcfg.Template{ID: id, Media: localcfg.MediaImage, Model: localcfg.Model{Media: localcfg.MediaImage, Workflow: &localcfg.Workflow{Graph: json.RawMessage(graph)}}}, true
	}
	models := &catalog.Catalog{
		Engine: catalog.Engine{Version: nodecat.Default().Python.ComfyUI},
		Models: []catalog.Model{{ID: "gguf-model", Workflows: []catalog.Workflow{{Name: "gguf-model", Template: "t"}}}},
	}
	assert.Contains(t, joined(nodecat.Validate(nodecat.Default(), models, unnamed)), "the graph uses classes of the node comfyui-gguf, which requires_nodes does not name")
}
