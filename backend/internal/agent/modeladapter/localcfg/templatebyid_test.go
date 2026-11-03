package localcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// A template found by ID is the caller's own: changing it does not change what the next lookup returns (Issue #996: the lookup reads one embedded
// file, it does not keep a shared copy), and an ID that no file has is not found.
func TestTemplateByIDReturnsACopyOfItsOwn(t *testing.T) {
	first, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	label, graph := first.Label, string(first.Model.Workflow.Graph)
	first.Label = "changed"
	first.Model.Workflow.Graph[0] = ' '
	again, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	assert.Equal(t, label, again.Label)
	assert.Equal(t, graph, string(again.Model.Workflow.Graph))
	_, ok = localcfg.TemplateByID("no-such-template")
	assert.False(t, ok)
}
