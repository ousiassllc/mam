package nodecat_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
)

func TestTheEmbeddedAllowlistIsValid(t *testing.T) {
	assert.Empty(t, nodecat.Validate(nodecat.Default(), catalog.Default(), localcfg.TemplateByID))
}

// The pins and the answers of the first entries, as read from the repositories (2026-11-03).
func TestTheAllowlistHoldsTheDecidedNodes(t *testing.T) {
	c := nodecat.Default()
	require.Len(t, c.Nodes, 3) // ipadapter-plus, controlnet-aux and vnccs-utils (vnccs_utils_test.go)

	ip, ok := c.Node("ipadapter-plus")
	require.True(t, ok)
	assert.Equal(t, "a0f451a5113cf9becb0847b92884cb10cbdec0ef", ip.Commit)
	assert.Equal(t, "https://github.com/cubiq/ComfyUI_IPAdapter_plus/archive/a0f451a5113cf9becb0847b92884cb10cbdec0ef.zip", ip.ArchiveURL)
	assert.Equal(t, "c6c49c82aa65cb96b93bdf9f9b547f9c95310a2668a7a9aaa0285cccf4590347", ip.ArchiveSHA256)
	assert.Equal(t, "ComfyUI_IPAdapter_plus-a0f451a5113cf9becb0847b92884cb10cbdec0ef", ip.ArchiveRoot())
	assert.True(t, ip.Installable(), "everything it imports is in the bundled environment")
	assert.Equal(t, modellicense.Yes, ip.License.Commercial)
	assert.Contains(t, ip.NodeClasses, "IPAdapterAdvanced")

	aux, ok := c.Node("controlnet-aux")
	require.True(t, ok)
	assert.False(t, aux.Installable(), "it needs OpenCV, mediapipe, onnxruntime...: not in the bundled environment, and mam runs no pip")
	assert.Equal(t, modellicense.Unknown, aux.License.Commercial, "the licenses of the weights it fetches at run time are not confirmed")
	assert.Contains(t, aux.InstallBlocked, "pip")

	by, ok := c.ByFolder("ComfyUI_IPAdapter_plus")
	require.True(t, ok)
	assert.Equal(t, "ipadapter-plus", by.ID)
	_, ok = c.ByFolder("evil")
	assert.False(t, ok)
}

// validNode is an entry that passes; each case of the next test breaks one thing.
func validNode() nodecat.Node {
	n, _ := nodecat.Default().Node("ipadapter-plus")
	return n
}

func validate(n nodecat.Node, python ...string) []error {
	c := &nodecat.Catalog{Nodes: []nodecat.Node{n}, Python: nodecat.Default().Python}
	c.Python.Packages = append(append([]string{}, c.Python.Packages...), python...)
	return nodecat.Validate(c, &catalog.Catalog{Engine: catalog.Engine{Version: c.Python.ComfyUI}}, localcfg.TemplateByID)
}

func TestAnEntryNeedsItsPinsLicenseDependenciesAndReview(t *testing.T) {
	require.Empty(t, validate(validNode()))
	for name, c := range map[string]struct {
		edit func(*nodecat.Node)
		want string
	}{
		"a branch, not a commit":                       {func(n *nodecat.Node) { n.Commit = "main" }, "commit must be 40"},
		"an archive of another commit":                 {func(n *nodecat.Node) { n.ArchiveURL = n.Repo + "/archive/refs/heads/main.zip" }, "archive_url"},
		"no archive sha256":                            {func(n *nodecat.Node) { n.ArchiveSHA256 = "" }, "archive_sha256"},
		"a folder that leaves custom_nodes":            {func(n *nodecat.Node) { n.Folder = "../x" }, "folder"},
		"a folder ComfyUI skips":                       {func(n *nodecat.Node) { n.Folder = "x.disabled" }, "folder"},
		"a license text of the branch":                 {func(n *nodecat.Node) { n.License.URL = n.Repo + "/blob/main/LICENSE" }, "pinned commit"},
		"no license name":                              {func(n *nodecat.Node) { n.License.Name = "" }, "license"},
		"an unknown answer without notice":             {func(n *nodecat.Node) { n.License.Commercial = modellicense.Unknown; n.License.Notice = "" }, "notice"},
		"no dependencies listed":                       {func(n *nodecat.Node) { n.PythonRequires = nil }, "python_requires"},
		"a package the environment lacks, installable": {func(n *nodecat.Node) { n.PythonRequires = append(n.PythonRequires, "cv2") }, "install_blocked"},
		"blocked without a reason":                     {func(n *nodecat.Node) { n.PythonRequires = append(n.PythonRequires, "cv2"); n.InstallBlocked = " " }, "install_blocked must say why"},
		"no review":                                    {func(n *nodecat.Node) { n.Review = "" }, "review"},
		"no node classes":                              {func(n *nodecat.Node) { n.NodeClasses = nil }, "node_classes"},
	} {
		n := validNode()
		c.edit(&n)
		errs := validate(n)
		require.NotEmpty(t, errs, name)
		assert.Contains(t, joined(errs), c.want, name)
	}
	// the same package is fine once the environment has it
	n := validNode()
	n.PythonRequires = append(n.PythonRequires, "cv2")
	assert.Empty(t, validate(n, "cv2"))
}

func joined(errs []error) string {
	var parts []string
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "\n")
}

// A model catalog entry whose workflow uses the classes of a custom node has to say so in requires_nodes, and a model with a file in the
// folder a node adds needs a workflow that requires the node: otherwise the model would install and then fail at the first run.
func TestTemplatesAndModelsMustNameTheNodesTheyUse(t *testing.T) {
	c := nodecat.Default()
	models := catalog.Default()
	tpl, ok := localcfg.TemplateByID("txt2img-sdxl-ipadapter")
	require.True(t, ok)
	require.Equal(t, []string{"ipadapter-plus"}, tpl.Model.Workflow.RequiresNodes)

	without := func(id string) (localcfg.Template, bool) {
		t, ok := localcfg.TemplateByID(id)
		if ok && id == "txt2img-sdxl-ipadapter" {
			w := *t.Model.Workflow
			w.RequiresNodes = nil
			t.Model = localcfg.Model{Media: t.Model.Media, Workflow: &w}
		}
		return t, ok
	}
	errs := joined(nodecat.Validate(c, models, without))
	assert.Contains(t, errs, "the graph uses classes of the node ipadapter-plus, which requires_nodes does not name")
	assert.Contains(t, errs, "no workflow of the model requires that node")

	blockedTemplate := func(id string) (localcfg.Template, bool) {
		t, ok := localcfg.TemplateByID(id)
		if ok && id == "txt2img-sdxl-ipadapter" {
			w := *t.Model.Workflow
			w.RequiresNodes = []string{"ipadapter-plus", "controlnet-aux", "nope"}
			t.Model = localcfg.Model{Media: t.Model.Media, Workflow: &w}
		}
		return t, ok
	}
	errs = joined(nodecat.Validate(c, models, blockedTemplate))
	assert.Contains(t, errs, "which mam cannot install", "a template cannot depend on a node that is blocked")
	assert.Contains(t, errs, `"nope", which is not in the allowlist`)
}
