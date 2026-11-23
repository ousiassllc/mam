package texttest_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var headingTag = regexp.MustCompile(`<h([1-6])[ >]`)

// skippedHeadings lists the headings of a screen that go down more than one level from the one before ("h1 → h3"): the tag is the structure of the document and the
// size is a class (T7). A fragment (a dialog) starts at the level of its own first heading.
func skippedHeadings(page string) []string {
	var found []string
	prev := 0
	for _, m := range headingTag.FindAllStringSubmatch(page, -1) {
		level := int(m[1][0] - '0')
		if prev != 0 && level > prev+1 {
			found = append(found, fmt.Sprintf("<h%d> after <h%d>", level, prev))
		}
		prev = level
	}
	return found
}

// A heading level is not skipped on any rendered screen or dialog (T7): a section under the h1 is an h2, whatever size it is set in.
func TestRendered_HeadingsDoNotSkipALevel(t *testing.T) {
	screens := renderedScreens(t)
	require.Greater(t, len(screens), 40, "the screens and the dialogs they open are rendered")
	for name, page := range screens {
		for _, found := range skippedHeadings(page) {
			t.Errorf("%s: %s: a heading level is not skipped (T7)", name, found)
		}
	}
}
