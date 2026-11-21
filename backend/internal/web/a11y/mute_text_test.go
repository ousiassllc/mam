package a11y_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A control that says nothing about its colour takes the guide's (C3): the focus ring of a native checkbox, radio, file input and video's
// controls is the same 2px --focus ring as a button's, and the fill of a checked box is --accent, not the browser's own blue.
func TestTokens_NativeControlsTakeTheFocusRingAndTheAccent(t *testing.T) {
	css := readTokens(t)
	assert.Regexp(t, `(?s):where\([^)]*input:is\(\[type="checkbox"\], \[type="radio"\], \[type="file"\]\)[^)]*video\[controls\]\):focus-visible \{\s*outline: 2px solid var\(--focus\);\s*outline-offset: 2px;`, css)
	assert.Regexp(t, `(?s):root \{\s*accent-color: var\(--accent\);`, css)
}

var (
	mutedText    = regexp.MustCompile(`(?s)<(?:p|li)\b[^>]*\btext-mute\b[^>]*>(.*?)</(?:p|li)>`)
	interpolated = regexp.MustCompile(`\$\{[^}]*\}|\{[^}]*\}`)
	markup       = regexp.MustCompile(`<[^>]+>`)
)

// --mute is for a supporting line (C6): a paragraph or list item that says two sentences or more is a text to read (a warning, a step, a rule,
// an empty state that says what to do next), and is --ink. A one-sentence caption stays --mute.
func TestSources_ParagraphsOfSeveralSentencesAreNotMute(t *testing.T) {
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, m := range mutedText.FindAllStringSubmatchIndex(string(src), -1) {
			body := string(src)[m[2]:m[3]]
			text := markup.ReplaceAllString(interpolated.ReplaceAllString(body, ""), "")
			if sentences := strings.Count(text, "。"); sentences >= 2 {
				line := strings.Count(string(src)[:m[0]], "\n") + 1
				t.Errorf("%s:%d: a text of %d sentences is text-mute: a text to read is text-ink (C6); only a one-line caption is text-mute", path, line, sentences)
			}
		}
	}
}
