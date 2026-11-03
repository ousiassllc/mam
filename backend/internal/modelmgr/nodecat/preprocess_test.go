package nodecat_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
)

// #993・#997: the nodes that need Python packages the bundled environment lacks stay on the allowlist as a pinned record and are not installable,
// and each says why in terms the screen shows. mam runs no pip (custom-nodes.md §5.1).
func TestNodesThatNeedMissingPackagesStayBlockedAndSayWhy(t *testing.T) {
	c := nodecat.Default()

	aux, ok := c.Node("controlnet-aux")
	require.True(t, ok)
	assert.False(t, aux.Installable())
	assert.Contains(t, aux.InstallBlocked, "imgprep", "Canny is made inside mam instead (#993)")
	assert.Contains(t, aux.Description, "reference_preprocess")

	pulid, ok := c.Node("pulid-flux-chroma")
	require.True(t, ok)
	assert.False(t, pulid.Installable())
	assert.Contains(t, pulid.InstallBlocked, "非商用", "InsightFace's weights are for non-commercial research only (#997)")
	assert.Contains(t, pulid.InstallBlocked, "insightface")
	assert.NotContains(t, c.Python.Packages, "insightface", "the bundled environment is not extended")
}

func TestNoNodeNeedingMissingPackagesIsInstallable(t *testing.T) {
	c := nodecat.Default()
	for _, n := range c.Nodes {
		for _, p := range n.PythonRequires {
			if !c.Python.Has(p) {
				assert.Falsef(t, n.Installable(), "%s imports %s, which the bundled Python lacks", n.ID, p)
			}
		}
	}
}
