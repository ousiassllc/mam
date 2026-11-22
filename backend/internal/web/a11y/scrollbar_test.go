package a11y_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/a11y/uisrc"
)

// The scrollbar of the Ousiass UI guide (§10, B1-B6): one rule for every scrolling area in static/src/css/scrollbar.css, in the order that works
// in Chromium (the standard pair is put back to `auto` inside @supports, or ::-webkit-scrollbar is ignored) and only without forced colours.
func TestScrollbarCSS_IsTheGuidesDesign(t *testing.T) {
	raw, err := os.ReadFile("../../../../static/src/css/scrollbar.css")
	require.NoError(t, err)
	css := string(raw)
	forced := strings.Index(css, "@media (forced-colors: none) {")
	supports := strings.Index(css, "@supports selector(::-webkit-scrollbar) {")
	require.True(t, forced >= 0 && supports > forced, "the @supports is inside the forced-colors: none media")
	standard, chromium := css[forced:supports], css[supports:]
	assert.Contains(t, standard, "scrollbar-width: thin;")
	assert.Contains(t, standard, "scrollbar-color: var(--scrollbar-thumb) transparent;")
	for _, want := range []string{
		"scrollbar-width: auto;", "scrollbar-color: auto;",
		"width: 8px;", "height: 8px;", "background: transparent;", "display: none;",
		"background: var(--scrollbar-thumb);", "border-radius: 999px;",
		"::-webkit-scrollbar-thumb:hover", "var(--scrollbar-thumb-hover)", "::-webkit-scrollbar-thumb:active", "var(--scrollbar-thumb-active)",
	} {
		assert.Contains(t, chromium, want)
	}
	assert.NotContains(t, css, "scrollbar-width: none", "a scrollbar is never hidden")
	for _, want := range []string{
		"--scrollbar-thumb: var(--mute);",
		"--scrollbar-thumb-hover: color-mix(in srgb, var(--ink) 35%, var(--mute));",
		"--scrollbar-thumb-active: color-mix(in srgb, var(--ink) 65%, var(--mute));",
	} {
		assert.Contains(t, uisrc.ReadTokens(t), want)
	}
}
