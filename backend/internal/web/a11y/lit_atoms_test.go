package a11y_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/a11y/uisrc"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// The Lit parts draw their badges and buttons from lib/ui/badge.ts and lib/ui/button.ts, the templates from atoms/badge.templ and atoms/button.templ: one
// look, written in two languages (W3, W6). These tests hold the words of the two together, as TestSources_LinkButtonLookIsWrittenOnce does for LINK_BUTTON.

// tsConst is the value of `export const <name> = ...;` in a file of lib/ui: the string literals ('a ' + 'b') joined, or, for an object, the literal of each key.
func tsConst(t *testing.T, file, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../static/src/components/lib/ui/" + file)
	require.NoError(t, err)
	src := string(raw)
	start := strings.Index(src, "export const "+name+" ")
	require.GreaterOrEqual(t, start, 0, "%s declares %s", file, name)
	decl := src[start:]
	return decl[:strings.Index(decl, ";\n")]
}

func tsWords(t *testing.T, file, name string) []string {
	t.Helper()
	var words []string
	for _, m := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(tsConst(t, file, name), -1) {
		words = append(words, strings.Fields(m[1])...)
	}
	return words
}

func tsTones(t *testing.T, file, name string) map[string][]string {
	t.Helper()
	tones := map[string][]string{}
	for _, m := range regexp.MustCompile(`(\w+): '([^']*)'`).FindAllStringSubmatch(tsConst(t, file, name), -1) {
		tones[m[1]] = strings.Fields(m[2])
	}
	return tones
}

// classOf is the class of the first element of a rendered component.
func classOf(t *testing.T, html string) []string {
	t.Helper()
	m := regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(html)
	require.NotNil(t, m, html)
	return strings.Fields(m[1])
}

func without(words []string, drop ...string) []string {
	var out []string
	for _, w := range words {
		if !slices.Contains(drop, w) {
			out = append(out, w)
		}
	}
	return out
}

// BADGE and BADGE_TONE are the class of atoms.Badge, word for word: the shape and the colours of every tone the Lit parts have.
func TestLitBadge_IsTheClassOfAtomsBadge(t *testing.T) {
	tones := tsTones(t, "badge.ts", "BADGE_TONE")
	byTone := map[string]atoms.BadgeTone{"info": atoms.BadgeInfo, "warning": atoms.BadgeWarn, "success": atoms.BadgeOK, "neutral": atoms.BadgeNeutral}
	require.Len(t, tones, len(byTone), "every tone of BADGE_TONE is paired with its tone of atoms here")
	shape := tsWords(t, "badge.ts", "BADGE")
	for name, tone := range byTone {
		want := classOf(t, uisrc.Render(t, atoms.Badge(tone, "x")))
		assert.ElementsMatch(t, want, append(append([]string{}, shape...), tones[name]...), "BADGE + BADGE_TONE.%s is the class of atoms.Badge(%s)", name, name)
	}
}

// BUTTON and BUTTON_TONE are the class of atoms.SmallButton. The Lit button adds what the markup of the template says in an attribute or in tokens.css: the
// focus ring and the look of aria-disabled (a button that stays focusable).
func TestLitButton_IsTheClassOfAtomsSmallButton(t *testing.T) {
	litOnly := []string{"focus-visible:focus-ring", "aria-disabled:cursor-not-allowed", "aria-disabled:opacity-40"}
	tones := tsTones(t, "button.ts", "BUTTON_TONE")
	byTone := map[string]atoms.ButtonTone{"primary": atoms.TonePrimary, "neutral": atoms.ToneNeutral}
	require.Len(t, tones, len(byTone), "every tone of BUTTON_TONE is paired with its tone of atoms here")
	shape := without(tsWords(t, "button.ts", "BUTTON"), litOnly...)
	for name, tone := range byTone {
		want := strings.Fields(atoms.SmallButtonClass(tone))
		assert.ElementsMatch(t, want, append(append([]string{}, shape...), tones[name]...), "BUTTON + BUTTON_TONE.%s is the class of atoms.SmallButtonClass(%s)", name, name)
	}
	for _, w := range litOnly {
		assert.Contains(t, tsWords(t, "button.ts", "BUTTON"), w, "the Lit button keeps %s", w)
	}
}
