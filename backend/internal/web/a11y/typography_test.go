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

// brandNameWeight is the one place that has a weight outside Regular (400) and SemiBold (600): the product name in nav.templ is font-light
// for the brand wordmark (Sora Light 300), the thinness of the words of the logo it replaced (#1171). No other exception is allowed.
const brandNameWeight = "../organisms/sidenav/nav.templ|light"

// A size that is not in the guide's scale, and a weight that is not Regular (400) or SemiBold (600), do not come back (T2, T3), but for brandNameWeight.
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
			if m := weight.FindStringSubmatch(line); m != nil && path+"|"+m[1] != brandNameWeight {
				t.Errorf("%s:%d: font-%s: the weights are Regular (400) and SemiBold (600, headings and buttons only)", path, n+1, m[1])
			}
		}
	}
}

// <strong> is SemiBold (T2): Tailwind's preflight would make it bolder (700), and the class check above does not see an element's default weight.
func TestTokens_StrongIsSemiBold(t *testing.T) {
	assert.Regexp(t, `(?s)@layer base \{.*\bstrong \{\s*font-weight: 600;`, readTokens(t))
}

// A <th scope="row"> and a <legend> are Regular (T2): SemiBold is for headings, buttons and the header row of a table, and the first column
// of a table is a name in the middle of the body text. The class check above cannot tell which element carries font-semibold.
func TestSources_RowHeadsAndLegendsAreRegular(t *testing.T) {
	semibold := regexp.MustCompile(`(?:^|[^\w-])font-semibold(?:[^\w-]|$)`)
	element := regexp.MustCompile(`<(?:th\b[^>]*scope="row"|legend\b)[^>]*>`)
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			for _, tag := range element.FindAllString(line, -1) {
				if semibold.MatchString(tag) {
					t.Errorf("%s:%d: %s is SemiBold: a row head and a legend are Regular (T2)", path, n+1, tag)
				}
			}
		}
	}
}

// Every <summary> is written with atoms.SummaryClass (O8: the heading of an accordion is SemiBold, text-sm text-ink, with the focus ring): a <summary> with
// a class of its own is the same part reading two ways.
func TestSources_EverySummaryTakesTheSummaryClass(t *testing.T) {
	summary := regexp.MustCompile(`<summary\b[^>]*>`)
	for _, path := range colorSources(t) {
		if strings.HasSuffix(path, ".css") {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			for _, tag := range summary.FindAllString(line, -1) {
				if !strings.Contains(tag, "atoms.SummaryClass") {
					t.Errorf("%s:%d: %s: a <summary> takes class={ atoms.SummaryClass } (O8)", path, n+1, tag)
				}
			}
		}
	}
}
