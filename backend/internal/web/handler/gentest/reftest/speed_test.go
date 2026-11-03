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
// modes, lists their labels after 「モデルの既定」, and is not drawn on the image form while no image model offers any (#991).
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

	assert.NotContains(t, newScreen(t).page(paths.GenerationTest()+"?media=video"), "gt-speed-mode", "no model with speed modes, no select")
}

// The image form offers the speed modes of the image models too (#991: Qwen-Image-Edit 2511 + Lightning), not those of the video models, and
// a speed mode the image form posts reaches the confirmation.
func TestTheImageFormOffersTheSpeedModesOfTheImageModels(t *testing.T) {
	s := newScreen(t, gentesttest.WithSpeedModel())

	image := s.page(paths.GenerationTest() + "?tier=detail&media=image")
	sel := regexp.MustCompile(`(?s)<select[^>]*data-testid="gt-speed-mode".*?</select>`).FindString(image)
	require.NotEmpty(t, sel, "the speed select is on the image form")
	for _, want := range []string{`<option value="standard"`, `<option value="lightning-8"`, `<option value="lightning-4"`, "4 ステップ（Lightning）"} {
		assert.Contains(t, sel, want)
	}
	assert.NotContains(t, sel, `value="fast"`, "the video model's mode is not offered for images")
	assert.Contains(t, image, `data-sampling-locked="true"`, "the card tells the script that the mode owns the steps and the cfg")

	form := url.Values{"media": {"image"}, "model": {string(gentesttest.ImageSpeedModel)}, "prompt": {"a cat"}, "speed_mode": {"lightning-4"}}
	assert.Equal(t, "lightning-4", s.confirmed(form).Get("speed_mode"))
}

// The mode chosen travels through the estimate to the confirmation (a hidden field the run posts back) and into the batch.
func TestTheChosenSpeedModeReachesTheConfirmationAndTheBatch(t *testing.T) {
	s := newScreen(t, gentesttest.WithSpeedModel())
	// the model is an image-to-video one: it needs its first frame (#1009)
	form := url.Values{"media": {"video"}, "model": {string(gentesttest.SpeedModel)}, "prompt": {"waves"}, "speed_mode": {"standard"}, "ref": {"upload::#1"}, "role_upload::#1": {"first_frame"}}

	hidden := s.confirmed(form, gentesttest.PNG)
	assert.Equal(t, "standard", hidden.Get("speed_mode"))

	form.Del("speed_mode")
	assert.Empty(t, s.confirmed(form, gentesttest.PNG).Get("speed_mode"), "no mode chosen: nothing is carried (each model's default)")
}
