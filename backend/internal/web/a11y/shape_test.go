package a11y_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shape, the layers and the motion of the Ousiass UI guide (S1-S3, O4, M1). The values are in static/src/css/tokens.css; a class that is not
// one of them does not compile there, and the sources are held to the same names here.
func TestTokens_RadiiShadowLayersAndMotionAreTheGuides(t *testing.T) {
	css := readTokens(t)
	for _, want := range []string{
		"--radius-*: initial;", "--radius-sm: 0.25rem;", "--radius-md: 0.5rem;", "--radius-lg: 0.75rem;", "--radius-xl: 1rem;",
		"--shadow-*: initial;", "--shadow-modal: 0 1px 2px rgb(0 0 0 / 0.06);",
		"--z-dropdown: 100;", "--z-sticky: 200;", "--z-drawer: 300;", "--z-modal: 400;", "--z-toast: 500;",
		"--motion-fast: 120ms;", "--motion-base: 180ms;", "--motion-slow: 280ms;", "--motion-fade-in: 320ms;", "--motion-fade-out: 240ms;",
		"--ease-out: cubic-bezier(0.2, 0.8, 0.2, 1);", "--ease-in: cubic-bezier(0.4, 0, 1, 1);",
		"--default-transition-duration: var(--motion-base);", "--default-transition-timing-function: var(--ease-out);",
		"--menu-item-h: 2.5rem;",
	} {
		assert.Contains(t, css, want)
	}
}

var (
	bareRounded = regexp.MustCompile(`(?:^|[^\w.-])(?:[a-z0-9\[\]-]+:)*rounded(?:[^\w-]|$)`)
	otherRadius = regexp.MustCompile(`(?:^|[^\w.-])(?:[a-z0-9\[\]-]+:)*rounded(?:-[a-z]+)?-(2xl|3xl)(?:[^\w-]|$)`)
	shadowUtil  = regexp.MustCompile(`(?:^|[^\w.-])(?:[a-z0-9\[\]-]+:)*(?:shadow|drop-shadow|inset-shadow)(?:-(?:xs|sm|md|lg|xl|2xl|\[[^\]]*\]))?(?:[^\w-]|$)`)
	numericZ    = regexp.MustCompile(`(?:^|[^\w.-])(?:[a-z0-9\[\]-]+:)*-?z-(?:\d+|\[[^\]]*\]|auto)(?:[^\w-]|$)`)
	plainMotion = regexp.MustCompile(`(?:\s|class=")(?:[a-z0-9\[\]-]+:)*transition(?:-all|-shadow|-transform)?[ "'}]`)
	halfSteps   = regexp.MustCompile(`(?:^|[^\w.-])(?:[a-z0-9\[\]-]+:)*-?(?:p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|gap|gap-x|gap-y|space-x|space-y)-\d+\.5(?:[^\w.-]|$)`)
)

// A corner, a shadow, a layer, a transition and a gap that the guide has no name for do not come back: the radii are 4 / 8 / 12 / 16 (rounded-sm to
// rounded-xl), the shadow is the modal's, the layers are the z-* names, a transition says what it moves (colour and background) and the gaps are
// steps of 4px.
func TestSources_UseOnlyTheGuidesShapeLayersAndMotion(t *testing.T) {
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".go") && !strings.Contains(path, "format") && !strings.Contains(path, "button") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || strings.HasPrefix(strings.TrimSpace(line), "*") || strings.HasPrefix(strings.TrimSpace(line), "/*") {
				continue
			}
			for re, why := range map[*regexp.Regexp]string{
				bareRounded: "a bare `rounded` is 4px by accident: use rounded-sm (tag, check) / rounded-md (input, button) / rounded-lg (card) / rounded-xl (modal)",
				otherRadius: "rounded-2xl and rounded-3xl are not in the guide (4 / 8 / 12 / 16 / full)",
				shadowUtil:  "the only shadow is the modal's (shadow-modal on a <dialog>); a card is told by its border",
				numericZ:    "layers are z-dropdown / z-sticky / z-drawer / z-modal / z-toast (z-raised inside a part), not a number",
				plainMotion: "a transition moves colour and background only (transition-colors), in --motion-base",
				halfSteps:   "gaps and paddings are steps of 4px: no .5 steps",
			} {
				if m := re.FindString(line); m != "" {
					t.Errorf("%s:%d: %q: %s", path, n+1, strings.TrimSpace(m), why)
				}
			}
		}
	}
}
