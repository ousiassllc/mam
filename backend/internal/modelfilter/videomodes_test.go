package modelfilter_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

func videoItem(id string, modes ...traits.VideoMode) modelfilter.Item {
	return item(id, func(i *modelfilter.Item) {
		i.Kinds, i.VideoModes = []modelfilter.Kind{modelfilter.KindVideo}, modes
	})
}

// The video modes are any-of within the list; a model without a value (an image model, a cloud video model) matches none (Issue #1014).
func TestVideoModesAreAnyOfAndAModelWithNoneNeverMatches(t *testing.T) {
	t2v, i2v := videoItem("t2v", traits.ModeT2V), videoItem("i2v", traits.ModeI2V)
	both, flf := videoItem("both", traits.ModeT2V, traits.ModeI2V, traits.ModeFLF2V), videoItem("flf", traits.ModeFLF2V)
	cloud := videoItem("cloud")
	img := item("img", nil)
	all := []modelfilter.Item{t2v, i2v, both, flf, cloud, img}
	assert.Empty(t, apply(t, modelfilter.Condition{VideoModes: []traits.VideoMode{traits.ModeVACE}}, all...), "no model has vace")
	assert.Equal(t, []string{"both", "flf"}, apply(t, modelfilter.Condition{VideoModes: []traits.VideoMode{traits.ModeFLF2V}}, all...))
	assert.Equal(t, []string{"t2v", "i2v", "both"}, apply(t, modelfilter.Condition{VideoModes: []traits.VideoMode{traits.ModeI2V, traits.ModeT2V}}, all...), "any-of, not all-of; cloud and image models never match")
	assert.Equal(t, 1, modelfilter.Condition{VideoModes: []traits.VideoMode{traits.ModeT2V, traits.ModeI2V}}.Active(), "the list is one part")
}

func TestVideoModesQueryAndJSONRoundTripInFixedOrder(t *testing.T) {
	c, err := modelfilter.ParseQuery(url.Values{"video_modes": {"R2V, flf2v", "t2v", "flf2v"}})
	require.NoError(t, err)
	assert.Equal(t, []traits.VideoMode{traits.ModeT2V, traits.ModeFLF2V, traits.ModeR2V}, c.VideoModes, "the fixed order of the catalog, without repeats")
	assert.Equal(t, "t2v,flf2v,r2v", c.Query(modelfilter.Condition{}).Get("video_modes"))
	data, err := c.JSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), `"video_modes":["t2v","flf2v","r2v"]`)
	back, err := modelfilter.ParseJSON(data)
	require.NoError(t, err)
	assert.Equal(t, c, back)
}

func TestABadVideoModeIsNamedByItsKey(t *testing.T) {
	_, err := modelfilter.ParseQuery(url.Values{"video_modes": {"t2v,v2v"}})
	assert.ErrorContains(t, err, "video_modes:")
	_, err = modelfilter.ParseJSON([]byte(`{"video_modes":["nope"]}`))
	assert.ErrorContains(t, err, "video_modes")
}
