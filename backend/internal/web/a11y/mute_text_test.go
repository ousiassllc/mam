package a11y_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A control that says nothing about its colour takes the guide's (C3): the focus ring of a native checkbox, radio, file input and video's and audio's
// controls is the same 2px --focus ring as a button's, and the fill of a checked box is --accent, not the browser's own blue.
func TestTokens_NativeControlsTakeTheFocusRingAndTheAccent(t *testing.T) {
	css := readTokens(t)
	assert.Regexp(t, `(?s):where\([^)]*input:is\(\[type="checkbox"\], \[type="radio"\], \[type="file"\]\)[^)]*video\[controls\], audio\[controls\]\):focus-visible \{\s*outline: 2px solid var\(--focus\);\s*outline-offset: 2px;`, css)
	assert.Regexp(t, `(?s):root \{\s*accent-color: var\(--accent\);`, css)
}

// --mute is for a supporting line (C6): a text of two sentences or more is a text to read (a warning, a step, a rule, an empty state that says what to
// do next, a notice to agree to), and is --ink. A one-sentence caption stays --mute. The check is of the text, not of the tag that carries it: a <p>, a
// <li>, a <td>, a <span>, a <label>, and a text that is --mute only because the <div> around it is. The text written in the sources is checked here; the
// text that comes from a Go constant, a catalog JSON or a value is checked on the screen that renders it (TestRendered_TextOfSeveralSentencesIsNotMute).
func TestSources_TextOfSeveralSentencesIsNotMute(t *testing.T) {
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".go") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		tree := parseSource(string(src))
		for _, found := range mutedSentences(tree) {
			t.Errorf("%s: %s is text-mute: a text to read is text-ink (C6); only a one-line caption is text-mute", path, found)
		}
		for _, found := range mutedLists(tree) {
			t.Errorf("%s: %s: the items say their own colour, the list does not (C6)", path, found)
		}
	}
}

// The underline of a link is --mute whatever else the link says (O7): one rule in tokens.css, not a decoration-mute on every <a class="underline">.
func TestTokens_TheUnderlineOfALinkIsMute(t *testing.T) {
	assert.Regexp(t, `(?s)@layer base \{.*:where\(a, button\)\.underline \{\s*text-decoration-color: var\(--mute\);`, readTokens(t))
}
