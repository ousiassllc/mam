package reftest_test

import (
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The speed select (#930: Wan 2.2 14B + LightX2V, 4 steps or the usual 20) is drawn on the video form only when a video model offers speed
// modes, lists their labels after 「モデルの既定」, and never appears on the image form.
func TestTheVideoFormOffersTheSpeedModesOfTheModels(t *testing.T) {
	s := newScreen(t, gentesttest.WithSpeedModel())

	video := s.page(paths.GenerationTest() + "?media=video")
	sel := regexp.MustCompile(`(?s)<select[^>]*data-testid="gt-speed-mode".*?</select>`).FindString(video)
	require.NotEmpty(t, sel, "the speed select is on the video form")
	assert.Contains(t, sel, `name="speed_mode"`)
	assert.Contains(t, sel, "モデルの既定")
	assert.Contains(t, sel, `<option value="fast"`)
	assert.Contains(t, sel, `<option value="standard"`)
	assert.Contains(t, sel, "4 ステップ（速い）")
	assert.Contains(t, video, "速さのモード", "the form explains who the choice is for")

	assert.NotContains(t, s.page(paths.GenerationTest()+"?media=image"), "gt-speed-mode")
	assert.NotContains(t, newScreen(t).page(paths.GenerationTest()+"?media=video"), "gt-speed-mode", "no model with speed modes, no select")
}

// The mode chosen travels through the estimate to the confirmation (a hidden field the run posts back) and into the batch.
func TestTheChosenSpeedModeReachesTheConfirmationAndTheBatch(t *testing.T) {
	s := newScreen(t, gentesttest.WithSpeedModel())
	form := url.Values{"media": {"video"}, "model": {string(gentesttest.SpeedModel)}, "prompt": {"waves"}, "speed_mode": {"standard"}}

	hidden := s.confirmed(form)
	assert.Equal(t, "standard", hidden.Get("speed_mode"))

	form.Del("speed_mode")
	assert.Empty(t, s.confirmed(form).Get("speed_mode"), "no mode chosen: nothing is carried (each model's default)")
}
