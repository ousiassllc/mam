package modelmgr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
)

// #928: the catalog says whether each model may be used commercially and can be narrowed by it.
func TestCatalog_NarrowsByCommercialUse(t *testing.T) {
	env, _ := newEnv(t)

	var all modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{"commercial": "all"}, &all)
	assert.Equal(t, "all", all.CommercialFilter)
	require.GreaterOrEqual(t, len(all.Models), 3)

	var ok modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{"commercial": "ok"}, &ok)
	assert.Equal(t, "ok", ok.CommercialFilter)
	require.NotEmpty(t, ok.Models)
	assert.Less(t, len(ok.Models), len(all.Models), "m-c is 商用NG")
	for _, m := range ok.Models {
		assert.Equal(t, modellicense.Yes, m.License.Commercial, m.ID)
	}

	assert.Contains(t, env.CallError("list_model_catalog", map[string]any{"commercial": "maybe"}), "validation")
}

// The setting 商用OKのモデルだけを使う is kept, and decides what the lists start at when no argument is given.
func TestCommercialOnly_TheSettingNarrowsTheDefaultListAndAnArgumentOverridesIt(t *testing.T) {
	env, _ := newEnv(t)

	var off modelmgr.CommercialOnlyOutput
	env.CallOK("get_commercial_only", map[string]any{}, &off)
	assert.False(t, off.CommercialOnly, "off until the user turns it on")
	var before modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &before)
	assert.Equal(t, "all", before.CommercialFilter)

	var on modelmgr.CommercialOnlyOutput
	env.CallOK("set_commercial_only", map[string]any{"enabled": true}, &on)
	assert.True(t, on.CommercialOnly)
	env.CallOK("get_commercial_only", map[string]any{}, &on)
	assert.True(t, on.CommercialOnly, "kept")

	var narrowed modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &narrowed)
	assert.Equal(t, "ok", narrowed.CommercialFilter)
	assert.Less(t, len(narrowed.Models), len(before.Models))

	var widened modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{"commercial": "all"}, &widened)
	assert.Len(t, widened.Models, len(before.Models))

	env.CallOK("set_commercial_only", map[string]any{"enabled": false}, &on)
	assert.False(t, on.CommercialOnly)
}
