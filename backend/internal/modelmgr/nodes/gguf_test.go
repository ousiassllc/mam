package nodes_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// #1016: ComfyUI-GGUF of the embedded allowlist is shown as blocked with its reason, is refused before anything is fetched or written, and is
// never given to --whitelist-custom-nodes (its package gguf is not in the bundled Python, and mam runs no pip).
func TestTheBlockedComfyUIGGUFIsRefusedAndNeverEnabled(t *testing.T) {
	m := nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})
	m.FreeBytes = func(string) (uint64, error) { return 1 << 40, nil }

	st, err := m.Status("comfyui-gguf")
	require.NoError(t, err)
	assert.Equal(t, nodes.StateBlocked, st.State)
	assert.Contains(t, st.Node.InstallBlocked, "gguf")

	err = m.Install(context.Background(), "comfyui-gguf", nil, nil)
	require.ErrorIs(t, err, nodes.ErrBlocked)
	assert.Contains(t, err.Error(), "pip", "the reason is in the error the screen shows")
	assert.NoDirExists(t, m.Folder(st.Node))
	assert.Empty(t, m.Enabled())
}
