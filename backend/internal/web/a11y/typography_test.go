package a11y_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The type of the Ousiass UI guide (T1-T4): the fonts, the size scale with its line heights and the two weights. The values are written once, in
// static/src/css/tokens.css; the sources only name them.
func readTokens(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../static/src/css/tokens.css")
	require.NoError(t, err)
	return string(raw)
}

func TestTokens_FontsAndSizesAreTheGuides(t *testing.T) {
	css := readTokens(t)
	assert.Contains(t, css, `--font-sans: Sora, "Noto Sans JP", sans-serif;`)
	assert.Contains(t, css, `--font-mono: "JetBrains Mono", ui-monospace, monospace;`)
	assert.Contains(t, css, "--text-*: initial;", "Tailwind's own scale is off: text-4xl does not compile")
	for size, spec := range map[string]string{
		"xs": "0.75rem|1.6", "sm": "0.875rem|1.7", "base": "1rem|1.8", "lg": "1.125rem|1.7", "xl": "1.5rem|1.5", "2xl": "2rem|1.4", "3xl": "2.5rem|1.3",
	} {
		rem, line, _ := strings.Cut(spec, "|")
		assert.Contains(t, css, "--text-"+size+": "+rem+";", size)
		assert.Contains(t, css, "--text-"+size+"--line-height: "+line+";", size)
	}
}

var (
	textSize = regexp.MustCompile(`(?:^|[^\w-])((?:[a-z0-9\[\]-]+:)*)text-(\d?xl|[4-9]xl)(?:[^\w-]|$)`)
	weight   = regexp.MustCompile(`(?:^|[^\w-])(?:[a-z0-9\[\]-]+:)*font-(medium|bold|extrabold|black|light|thin)(?:[^\w-]|$)`)
)

// A size that is not in the guide's scale, and a weight that is not Regular (400) or SemiBold (600), do not come back (T2, T3).
func TestSources_UseOnlyTheGuidesSizesAndWeights(t *testing.T) {
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			for _, m := range textSize.FindAllStringSubmatch(line, -1) {
				if m[2] == "xl" || m[2] == "2xl" || m[2] == "3xl" {
					continue
				}
				t.Errorf("%s:%d: text-%s is not in the size scale of the guide (xs sm base lg xl 2xl 3xl)", path, n+1, m[2])
			}
			if m := weight.FindStringSubmatch(line); m != nil {
				t.Errorf("%s:%d: font-%s: the weights are Regular (400) and SemiBold (600, headings and buttons only)", path, n+1, m[1])
			}
		}
	}
}
