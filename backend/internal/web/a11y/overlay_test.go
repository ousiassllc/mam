package a11y_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// overlayAlpha is the opacity the mask painter draws its overlay at (static/src/components/mask-paint/mam-mask-paint.ts, `opacity: 0.55`).
const overlayAlpha = 0.55

// blendOver is what the picture shows under an overlay of `paint` drawn at overlayAlpha (the browser blends the channels as written).
func blendOver(paint, picture [3]float64) [3]float64 {
	var out [3]float64
	for i := range out {
		out[i] = overlayAlpha*paint[i] + (1-overlayAlpha)*picture[i]
	}
	return out
}

// The paint over a picture (--overlay-deep, --overlay-pale; color-rules.md §4.2) keeps 3:1 (WCAG 1.4.11) against the picture where it applies: the deep one on
// the bright pictures the painter gives it to, the pale one on the dark ones. The pictures are the ones the guide names; no colour at this opacity keeps 3:1 on a
// mid-tone, so the grey is not asserted (the token is only the one that does better there).
func TestOverlayTokens_Keep3To1OnThePictureTheyAreGivenTo(t *testing.T) {
	bright := map[string][3]float64{"white": {255, 255, 255}, "bright wall": {230, 220, 203}, "sky": {135, 206, 235}}
	dark := map[string][3]float64{"near black": {16, 16, 16}, "black": {0, 0, 0}}
	for _, theme := range []string{"light", "dark"} {
		deep, pale := tokenValue(t, theme, "overlay-deep"), tokenValue(t, theme, "overlay-pale")
		for name, picture := range bright {
			assert.GreaterOrEqual(t, contrast(blendOver(deep, picture), picture), 3.0, "--overlay-deep on %s (%s theme)", name, theme)
		}
		for name, picture := range dark {
			assert.GreaterOrEqual(t, contrast(blendOver(pale, picture), picture), 3.0, "--overlay-pale on %s (%s theme)", name, theme)
		}
	}
}

// The painter switches at a mean luminance of 0.11 (mask-draw.ts PALE_BELOW), where the two colours are equally visible on a grey: each does better than the other on its side.
func TestOverlayTokens_TheSwitchIsWhereTheTwoAreEquallyVisible(t *testing.T) {
	deep, pale := tokenValue(t, "light", "overlay-deep"), tokenValue(t, "light", "overlay-pale")
	for _, g := range []float64{16, 40} { // below the switch (L < 0.11)
		grey := [3]float64{g, g, g}
		assert.Greater(t, contrast(blendOver(pale, grey), grey), contrast(blendOver(deep, grey), grey), "grey %v", g)
	}
	for _, g := range []float64{128, 176, 255} { // above it
		grey := [3]float64{g, g, g}
		assert.Greater(t, contrast(blendOver(deep, grey), grey), contrast(blendOver(pale, grey), grey), "grey %v", g)
	}
}
