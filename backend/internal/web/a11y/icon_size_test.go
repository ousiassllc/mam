package a11y_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An Icon call that gives a literal class says its size with a token utility: size-icon-sm / -md / -lg (16 / 20 / 24, I2; -xl 32 is for an empty state),
// never h-4 w-4 and the like. The class may also be built (`"mt-1 size-icon-md shrink-0 "+tone`); only the literal part is read.
var (
	iconCall      = regexp.MustCompile(`@(?:atoms\.)?Icon\([A-Za-z.()]+, "([^"]*)"`)
	iconTokenSize = regexp.MustCompile(`(?:^|\s)size-icon-(?:sm|md|lg|xl)(?:\s|$)`)
	rawSize       = regexp.MustCompile(`(?:^|\s)(?:size|h|w)-\d`)
)

func TestSources_IconsAreAtTheGuidesSizes(t *testing.T) {
	for _, path := range colorSources(t) {
		if !strings.HasSuffix(path, ".templ") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			for _, m := range iconCall.FindAllStringSubmatch(line, -1) {
				if m[1] != "" && (!iconTokenSize.MatchString(m[1]) || rawSize.MatchString(m[1])) { // "" is the default size-icon-md
					t.Errorf("%s:%d: Icon class %q: an icon is size-icon-sm (16), size-icon-md (20) or size-icon-lg (24), not an h-/w- pair", path, n+1, m[1])
				}
			}
		}
	}
	spinner, err := os.ReadFile("../atoms/spinner.templ")
	require.NoError(t, err)
	assert.Contains(t, string(spinner), "size-icon-md", "the spinner is 20px")
}

// The sizes are tokens: --icon-* in @theme (16 / 20 / 24 / 32) and a size-icon-* utility for each, and the default of the Go and the Lit Icon is a utility too.
func TestTokens_IconSizesAreNamed(t *testing.T) {
	css := readTokens(t)
	for _, want := range []string{
		"--icon-sm: 1rem;", "--icon-md: 1.25rem;", "--icon-lg: 1.5rem;", "--icon-xl: 2rem;",
		"@utility size-icon-sm {", "@utility size-icon-md {", "@utility size-icon-lg {", "@utility size-icon-xl {",
		"width: var(--icon-sm);", "height: var(--icon-sm);", "width: var(--icon-lg);",
	} {
		assert.Contains(t, css, want)
	}
	goIcon, err := os.ReadFile("../atoms/icon.templ")
	require.NoError(t, err)
	assert.Contains(t, string(goIcon), `const iconDefaultClass = "size-icon-md shrink-0"`)
	litIcon, err := os.ReadFile("../../../../static/src/components/lib/icon.ts")
	require.NoError(t, err)
	assert.Contains(t, string(litIcon), "size = 'size-icon-sm'")
}
