package nodecat_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
)

// #985: VNCCS_Utils (PoseStudio) is on the allowlist with its pin, and is not installable: PoseStudio waits for the browser to send the
// pose, the node fetches pose libraries and models at run time without a pin, and the bundled environment lacks packages it uses.
func TestVNCCSUtilsIsAllowlistedButNotInstallable(t *testing.T) {
	c := nodecat.Default()
	n, ok := c.Node("vnccs-utils")
	require.True(t, ok)

	assert.Equal(t, "334eee549fba5025b97c4e0a6123b2b68a46f0fc", n.Commit)
	assert.Equal(t, "https://github.com/AHEKOT/ComfyUI_VNCCS_Utils/archive/334eee549fba5025b97c4e0a6123b2b68a46f0fc.zip", n.ArchiveURL)
	assert.Equal(t, "cc5567fc5bc705f0549c85336aac96df08b09db500a4f21a99318ad03fda4d04", n.ArchiveSHA256)
	assert.Equal(t, "ComfyUI_VNCCS_Utils", n.Folder)
	assert.Contains(t, n.NodeClasses, "VNCCS_PoseStudio")

	assert.False(t, n.Installable())
	assert.Contains(t, n.InstallBlocked, "ブラウザ")
	assert.Contains(t, n.InstallBlocked, "Hugging Face")
	assert.Contains(t, n.InstallBlocked, "cv2")

	assert.Equal(t, modellicense.Unknown, n.License.Commercial, "the code is MIT but the poses and models it fetches are not confirmed")
	assert.Contains(t, n.License.Notice, "配布元")

	by, ok := c.ByFolder("ComfyUI_VNCCS_Utils")
	require.True(t, ok)
	assert.Equal(t, "vnccs-utils", by.ID)
}
