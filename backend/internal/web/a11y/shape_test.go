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
	halfSteps   = regexp.MustCompile(`(?:^|[^\w.-])(?:[a-z0-9\[\]-]+:)*-?(?:p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|gap|gap-x|gap-y|space-x|space-y|inset|inset-x|inset-y|top|right|bottom|left|start|end)-\d+\.5(?:[^\w.-]|$)`)
	// 20px is not a step of the guide's scale (4 / 8 / 12 / 16 / 24 ...): a card, a dialog and a stack are p-4 / p-6 and gap-4 / gap-6, and the indent
	// of a list is pl-4 or pl-6 (the margins and the paddings of one side too).
	twentyStep = regexp.MustCompile(`(?:^|[^\w.-])(?:[a-z0-9\[\]-]+:)*(?:p|px|py|pt|pb|pl|pr|ml|mr|gap|gap-x|gap-y|space-x|space-y)-5(?:[^\w.-]|$)`)
	// The gutter of a screen is 16 on a phone and 24 from md up (S5), not 32: neither md:p-8 nor a <main> with a bare p-8.
	wideGutter = regexp.MustCompile(`(?:^|[^\w.-])md:(?:p|px)-8(?:[^\w.-]|$)|<main\b[^>]*[ "]p-8[ "]`)
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
				halfSteps:   "gaps, paddings and insets are steps of 4px: no .5 steps",
				twentyStep:  "20px is not in the spacing scale: use 16 (p-4 / gap-4) or 24 (p-6 / gap-6)",
				wideGutter:  "the gutter of a screen is p-4 md:p-6 (16 / 24), not md:p-8",
			} {
				if m := re.FindString(line); m != "" {
					t.Errorf("%s:%d: %q: %s", path, n+1, strings.TrimSpace(m), why)
				}
			}
		}
	}
}

var iconSize = regexp.MustCompile(`@(?:atoms\.)?Icon\([A-Za-z.]+, "[^"]*\b(?:h|w)-(?:3|3\.5|7|8|9|10)\b`)

// An icon is 16, 20 or 24px (h-4 / h-5 / h-6, I2), and the spinner is 20px.
func TestSources_IconsAreAtTheGuidesSizes(t *testing.T) {
	for _, path := range colorSources(t) {
		if !strings.HasSuffix(path, ".templ") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			if m := iconSize.FindString(line); m != "" {
				t.Errorf("%s:%d: %q: an icon is h-4 w-4 (16), h-5 w-5 (20) or h-6 w-6 (24)", path, n+1, m)
			}
		}
	}
	spinner, err := os.ReadFile("../atoms/spinner.templ")
	require.NoError(t, err)
	assert.Contains(t, string(spinner), "h-5 w-5", "the spinner is 20px")
}

// The motion of the overlays and of a press (M3-M5) is in static/src/css/motion.css: a fade in with --ease-out and one out with --ease-in
// (a dialog, a popover, the scrim, a toast), the clip of the ripple, and no motion beyond opacity under reduced motion.
func TestMotionCSS_FadesTheOverlaysAndClipsTheRipple(t *testing.T) {
	raw, err := os.ReadFile("../../../../static/src/css/motion.css")
	require.NoError(t, err)
	css := string(raw)

	for _, want := range []string{
		"opacity var(--motion-fade-out) var(--ease-in)", "opacity var(--motion-fade-in) var(--ease-out)",
		"allow-discrete", "@starting-style", "dialog::backdrop", "var(--scrim)",
		"[data-toast][data-leaving]", "[data-alert][data-leaving]", "[data-connection-lost][data-leaving]", "[data-ripple]", "overflow: hidden", "@media (prefers-reduced-motion: reduce)",
	} {
		assert.Contains(t, css, want)
	}
	assert.NotRegexp(t, `animation|@keyframes|translate|scale`, css, "an overlay fades; it does not move or bounce (M2)")
}

// A list, a form, a section or a table that is a bordered panel of its own on the screen is a card: rounded-lg (S1). rounded-md is for what sits in a
// card (an input, a button, a nested face, an alert). A panel with a neutral border and rounded-md on one of these tags is the 2nd-run miss.
func TestSources_PanelsOfTheirOwnAreRoundedLg(t *testing.T) {
	panel := regexp.MustCompile(`(?s)<(?:ul|ol|form|section|table)\b[^<>]*\brounded-md\b[^<>]*\bborder-border\b[^<>]*>|<(?:ul|ol|form|section|table)\b[^<>]*\bborder-border\b[^<>]*\brounded-md\b[^<>]*>`)
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") {
			continue
		}
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, m := range panel.FindAllStringIndex(string(raw), -1) {
			t.Errorf("%s:%d: a bordered list / form / section / table is a panel of its own: rounded-lg (S1)", path, strings.Count(string(raw)[:m[0]], "\n")+1)
		}
	}
}

// A frame of its own that holds a title, a description or a body (a surface with a neutral border and a padding of 8px or more, on a li / div / span /
// section / article / details) is a card: rounded-lg (S1). rounded-md is for a chip (a short label), an input, a button and a nested face. The tags above
// (ul / form / ...) are held by TestSources_PanelsOfTheirOwnAreRoundedLg; the frame of a choice is held here too: pick.Card has no corner of its own, and
// every call names one (a chip rounded-md, a card rounded-lg), so that a card does not get the chip's corner by default.
func TestSources_CardsAreRoundedLg(t *testing.T) {
	card := regexp.MustCompile(`<(?:li|div|span|section|article|details|label)\b[^<>]*>`)
	pickCall := regexp.MustCompile(`@pick\.Card\((?s:.*?)\}, "([^"]*)"\)`)
	hasClass := func(tag, class string) bool {
		return regexp.MustCompile(`[ "']` + regexp.QuoteMeta(class) + `[ "']`).MatchString(tag)
	}
	padded := regexp.MustCompile(`[ "'](?:p|px|py)-[2-9][ "']`)
	// The bar of the weight of a word sits against the text it edits and is a toolbar, not a card: its frame is rounded-md by spec/components/shared-ui-parts.md §W1.
	const weightBar = "prompt-weight/mam-prompt-weight.ts"
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".go") || strings.HasSuffix(path, weightBar) {
			continue
		}
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		src := string(raw)
		for _, m := range card.FindAllStringIndex(src, -1) {
			tag := src[m[0]:m[1]]
			if hasClass(tag, "rounded-md") && hasClass(tag, "border-border") && hasClass(tag, "bg-surface") && padded.MatchString(tag) {
				t.Errorf("%s:%d: a bordered surface with padding is a card: rounded-lg (S1)", path, strings.Count(src[:m[0]], "\n")+1)
			}
		}
		for _, m := range pickCall.FindAllStringSubmatchIndex(src, -1) {
			if layout := src[m[2]:m[3]]; !strings.Contains(layout, "rounded-md") && !strings.Contains(layout, "rounded-lg") {
				t.Errorf("%s:%d: a pick.Card names its corner: rounded-md for a chip, rounded-lg for a card (S1)", path, strings.Count(src[:m[0]], "\n")+1)
			}
		}
	}
	frame, err := os.ReadFile("../molecules/pick/pick.templ")
	require.NoError(t, err)
	assert.NotContains(t, string(frame), `"block rounded-md border border-border"`, "the corner of a pick.Card is the caller's")
}

// The look of a button is written once (O7): atoms/button.templ for the templates and components/lib/button.ts for the Lit parts. A line that spells the
// main button (`bg-accent text-bone`) or the sub one (`border-border … hover:bg-accent-soft`) with the shape of a button (rounded-md font-semibold) again is a
// copy that a change to the guide would miss.
func TestSources_ButtonLookIsWrittenOnce(t *testing.T) {
	shape := regexp.MustCompile(`rounded-md[^"` + "`" + `]*font-semibold|font-semibold[^"` + "`" + `]*rounded-md`)
	main := regexp.MustCompile(`\bbg-accent\b[^"` + "`" + `]*\btext-bone\b`)
	sub := regexp.MustCompile(`\bborder-border\b[^"` + "`" + `]*\bhover:bg-accent-soft\b`)
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") || strings.HasSuffix(path, "atoms/button.templ") || strings.HasSuffix(path, "lib/button.ts") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			if shape.MatchString(line) && (main.MatchString(line) || sub.MatchString(line)) {
				t.Errorf("%s:%d: a button class written by hand: use atoms.ButtonClass / atoms.SmallButtonClass (templ) or BUTTON + BUTTON_TONE of lib/button.ts (Lit) (O7)", path, n+1)
			}
		}
	}
}

// An icon is an Icon (Iconoir, I1), not a character that stands for one: the arrow of a back link, the plus of an attachment, the star of a favourite, the
// cross of a close. A label that starts with an arrow, or a button whose only content is a plus, is a glyph a screen reader reads as "左矢印" and whose line
// does not follow the scale. Texts that use an arrow to say "to" (→ in a sentence) are prose and not held here.
func TestSources_IconsAreNotGlyphs(t *testing.T) {
	glyph := regexp.MustCompile(`["'>（]\s*[←↑↓×＋✕✖★☆]\s*(?:[^\s"'<）]|["'<）])`)
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") || strings.HasSuffix(path, "-fixtures.ts") { // a fixture is a stand-in for markup the server renders
			continue
		}
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(raw), "\n") {
			if text := strings.TrimSpace(line); strings.HasPrefix(text, "//") || strings.HasPrefix(text, "/*") || strings.HasPrefix(text, "*") {
				continue
			}
			if m := glyph.FindString(line); m != "" {
				t.Errorf("%s:%d: %q: a glyph is used as an icon: use atoms.Icon / lib/icon.ts (Iconoir, I1)", path, n+1, m)
			}
		}
	}
}
