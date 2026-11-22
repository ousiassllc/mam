package a11y_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	classAttr     = regexp.MustCompile(`class="([^"]*)"`)
	templCallName = regexp.MustCompile(`@([a-z]\w*)\(`)
)

// isGap2CardContainer is a line whose class attribute is a grid or a flex-wrap with gap-2.
func isGap2CardContainer(line string) bool {
	m := classAttr.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	var layout, gap bool
	for _, c := range strings.Fields(m[1]) {
		layout = layout || c == "grid" || c == "flex-wrap"
		gap = gap || c == "gap-2"
	}
	return layout && gap
}

// cardGridGaps are the lines of a templ source whose container (a grid or a flex-wrap with gap-2) holds framed cards: a child with rounded-lg, or a call to
// a templ of the same package whose root element is a rounded-lg frame (defined: the templ sources of the package). The container is the lines down to its
// closing tag (the first closing tag at its own indent or a shallower line).
func cardGridGaps(src string, defined map[string]string) []int {
	indent := func(s string) int { return len(s) - len(strings.TrimLeft(s, "\t ")) }
	lines := strings.Split(src, "\n")
	var found []int
	for i, line := range lines {
		if !isGap2CardContainer(line) {
			continue
		}
		for _, child := range lines[i+1:] {
			if strings.TrimSpace(child) != "" && (indent(child) < indent(line) || strings.HasPrefix(strings.TrimSpace(child), "</") && indent(child) <= indent(line)) {
				break
			}
			if strings.Contains(child, "rounded-lg") {
				found = append(found, i+1)
				break
			}
			if m := templCallName.FindStringSubmatch(child); m != nil && strings.Contains(defined[m[1]], "rounded-lg") {
				found = append(found, i+1)
				break
			}
		}
	}
	return found
}

// templRoots maps the name of each templ of a package to its first lines (the root element), to tell a call that renders a card.
func templRoots(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.templ"))
	require.NoError(t, err)
	decl := regexp.MustCompile(`^templ ([a-zA-Z]\w*)\(`)
	roots := map[string]string{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			if m := decl.FindStringSubmatch(line); m != nil {
				roots[m[1]] = strings.Join(lines[i+1:min(i+6, len(lines))], "\n")
			}
		}
	}
	return roots
}

// Framed cards (rounded-lg with a border) are set 12px apart at the least (the dense grid gap: S2, spec/components/shared-ui-parts.md): a grid or a flex-wrap
// of cards with gap-2 puts the frames 8px from each other. gap-2 stays for chips, toggles, a row of buttons, menu items and the fields of a form.
func TestSources_CardGridsAreNotGap2(t *testing.T) {
	roots := map[string]map[string]string{}
	for _, path := range colorSources(t) {
		if !strings.HasSuffix(path, ".templ") {
			continue
		}
		dir := filepath.Dir(path)
		if roots[dir] == nil {
			roots[dir] = templRoots(t, dir)
		}
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, n := range cardGridGaps(string(raw), roots[dir]) {
			t.Errorf("%s:%d: cards set in a grid / flex-wrap are 12px apart at the least: gap-3 (dense) or gap-4, not gap-2 (S2)", path, n)
		}
	}
}

func TestCardGridGaps_FindsCardsButNotChipsOrFormFields(t *testing.T) {
	card := "<ul class=\"grid grid-cols-2 gap-2 sm:grid-cols-4\">\n\t<li class=\"rounded-lg border border-border p-2\">x</li>\n</ul>\n"
	called := "<ul class=\"grid gap-2\">\n\t@row(r)\n</ul>\n"
	wrap := "<div class=\"flex flex-wrap gap-2\">\n\t<span class=\"rounded-lg border p-2\">x</span>\n</div>\n"
	chips := "<div class=\"flex flex-wrap gap-2\">\n\t<span class=\"rounded-md border px-2\">x</span>\n</div>\n<div class=\"grid gap-2 sm:grid-cols-2\">\n\t<label>name</label>\n</div>\n<div class=\"rounded-lg border\">\n</div>\n"
	dense := "<ul class=\"grid grid-cols-2 gap-3\">\n\t<li class=\"rounded-lg border p-2\">x</li>\n</ul>\n"
	defined := map[string]string{"row": "<li class=\"rounded-lg border p-2\">"}
	assert.Equal(t, []int{1}, cardGridGaps(card, defined))
	assert.Equal(t, []int{1}, cardGridGaps(called, defined))
	assert.Equal(t, []int{1}, cardGridGaps(wrap, defined))
	assert.Empty(t, cardGridGaps(chips, defined))
	assert.Empty(t, cardGridGaps(dense, defined))
}
