package engine_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// The engine is always started with --disable-all-custom-nodes; the folders the allowlist enabled are added to it as
// --whitelist-custom-nodes, before --base-directory so that the option does not take the next flag's value for a folder.
func TestCommandWhitelistsOnlyTheFoldersTheAllowlistEnabled(t *testing.T) {
	r := newRig(t, "v0.42.1")
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))

	for name, c := range map[string]struct {
		enabled func() []string
		want    []string
	}{
		"no provider":      {nil, nil},
		"nothing enabled":  {func() []string { return nil }, nil},
		"one node":         {func() []string { return []string{"ComfyUI_IPAdapter_plus"} }, []string{"--whitelist-custom-nodes", "ComfyUI_IPAdapter_plus"}},
		"two nodes (kept)": {func() []string { return []string{"a", "b"} }, []string{"--whitelist-custom-nodes", "a", "b"}},
	} {
		r.inst.CustomNodes = c.enabled
		spec, err := r.inst.Command(51234)
		require.NoError(t, err, name)
		want := append([]string{
			"-s", filepath.FromSlash("ComfyUI/main.py"), "--listen", "127.0.0.1", "--port", "51234", "--disable-auto-launch", "--disable-all-custom-nodes",
		}, c.want...)
		want = append(want, "--base-directory", r.inst.Layout.DataDir())
		assert.Equal(t, want, spec.Args, name)
		assert.Contains(t, spec.Args, "--disable-all-custom-nodes", name+": every other folder stays off")
	}
}
