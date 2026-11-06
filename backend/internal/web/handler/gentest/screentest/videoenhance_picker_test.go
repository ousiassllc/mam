package screentest_test

import (
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The model of "make the video larger / smoother" is the model picker every screen uses (Issue #1011), not a <select>: one field for each operation and each place
// it runs, with the model that can run now chosen and posted as `model`.
func TestTheModelOfTheVideoEnhancementIsAModelPickerFieldNotASelect(t *testing.T) {
	s := videoScreen(t, aCloud(true))
	body := s.page(paths.GenerationTestPart("results", s.aVideo()))

	local := regexp.MustCompile(`(?s)<form[^>]*gt-venh-local-upscale.*?</form>`).FindString(body)
	require.NotEmpty(t, local)
	assert.NotContains(t, local, "<select name=\"model\"")
	assert.Contains(t, local, `id="gt-venh-model-upscale-local"`)
	assert.Contains(t, local, paths.ModelPickerSource("video-enhance")+"?op=upscale")
	assert.Regexp(t, `<input type="hidden" name="model" value="`+regexp.QuoteMeta(string(gentesttest.UpscaleVideoModel))+`" data-mp-from="value">`, local)

	topaz := regexp.MustCompile(`(?s)<form[^>]*gt-venh-topaz-interpolate.*?</form>`).FindString(body)
	require.NotEmpty(t, topaz)
	assert.Contains(t, topaz, `id="gt-venh-model-interpolate-topaz"`)
	assert.Contains(t, topaz, "cloud=1")
	assert.Regexp(t, `<input type="hidden" name="model" value="`+regexp.QuoteMeta(topazApollo)+`" data-mp-from="value">`, topaz)
}

// The picker lists the models of the operation at that place only, as cards that put the model id into the field; the filter bar is the shared one.
func TestTheVideoEnhancementPickerListsTheModelsOfTheOperationAndPlaceAsCards(t *testing.T) {
	s := videoScreen(t, aCloud(true))
	picker := func(q url.Values) string { return s.page(paths.ModelPickerSource("video-enhance") + "?" + q.Encode()) }

	local := picker(url.Values{"op": {"upscale"}, "mf": {"1"}, "available_only": {"0"}})
	assert.Contains(t, local, string(gentesttest.UpscaleVideoModel), "the model of this PC that enlarges")
	assert.NotContains(t, local, string(gentesttest.InterpolateVideoModel), "another operation is not listed")
	assert.NotContains(t, local, topazProteus, "Topaz is not of this PC")
	assert.Contains(t, local, `data-mp-values`)
	assert.Contains(t, local, "mam-model-picker-bar", "the shared filter bar")

	cloud := picker(url.Values{"op": {"interpolate"}, "cloud": {"1"}, "mf": {"1"}, "available_only": {"0"}})
	assert.Contains(t, cloud, topazApollo)
	assert.Contains(t, cloud, "Topaz（有料クラウド）")
	assert.NotContains(t, cloud, topazProteus)
	assert.NotContains(t, cloud, string(gentesttest.InterpolateVideoModel))
}

// A Topaz model without its key is hidden by "選べるものだけ" (the start) and listed with its reason and the way to fix it once that is off.
func TestAnEnhancementModelWithoutItsKeyIsAnUnavailableCardWithTheReason(t *testing.T) {
	s := videoScreen(t, aCloud(false))
	q := url.Values{"op": {"upscale"}, "cloud": {"1"}}

	assert.NotContains(t, s.page(paths.ModelPickerSource("video-enhance")+"?"+q.Encode()), "Proteus", "the start hides what cannot be chosen")

	q.Set("mf", "1")
	q.Set("available_only", "0")
	shown := s.page(paths.ModelPickerSource("video-enhance") + "?" + q.Encode())
	assert.Contains(t, shown, "Proteus")
	assert.Contains(t, shown, "API キー")
	assert.Contains(t, shown, paths.SettingsCategory(paths.SettingsAPIKeys), "the way to register the key")
}
