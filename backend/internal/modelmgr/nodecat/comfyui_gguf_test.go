package nodecat_test

import (
	"encoding/json"
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

// A model that needs a node mam cannot install would be one that can never be installed, so the validation refuses a template that requires
// the blocked ComfyUI-GGUF, and the catalog holds no GGUF model until the node can be installed (custom-nodes.md §5.5).
func TestAGGUFTemplateCannotRequireTheBlockedNode(t *testing.T) {
	graph := `{"1": {"class_type": "UnetLoaderGGUF", "inputs": {"unet_name": "m.gguf"}}}`
	tpl := func(id string) (localcfg.Template, bool) {
		return localcfg.Template{ID: id, Media: localcfg.MediaImage, Model: localcfg.Model{Media: localcfg.MediaImage, Workflow: &localcfg.Workflow{
			Graph: json.RawMessage(graph), RequiresNodes: []string{"comfyui-gguf"},
		}}}, true
	}
	models := &catalog.Catalog{
		Engine: catalog.Engine{Version: nodecat.Default().Python.ComfyUI},
		Models: []catalog.Model{{ID: "gguf-model", Workflows: []catalog.Workflow{{Name: "gguf-model", Template: "t"}}}},
	}
	errs := joined(nodecat.Validate(nodecat.Default(), models, tpl))
	assert.Contains(t, errs, "requires the node comfyui-gguf, which mam cannot install")

	for _, m := range catalog.Default().Models {
		for _, f := range m.Files {
			assert.NotContains(t, f.Name, ".gguf", "model %s", m.ID)
		}
	}
}
