package a11y_test

import (
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// tokenValue is the colour of a design token (--mute, --bone, ...) in a theme, read from static/src/css/tokens.css, the one place
// the values are written: the `:root` block is the Light theme and the `:root[data-theme="dark"]` block the Dark one (a token the
// Dark block does not set is the same in both).
func tokenValue(t *testing.T, theme, name string) [3]float64 {
	t.Helper()
	raw, err := os.ReadFile("../../../../static/src/css/tokens.css")
	require.NoError(t, err)
	css := string(raw)
	blocks := []string{css[strings.Index(css, ":root {"):]}
	if theme == "dark" {
		blocks = []string{css[strings.Index(css, `:root[data-theme="dark"] {`):], blocks[0]}
	}
	for _, block := range blocks {
		block = block[:strings.Index(block, "}")]
		if m := regexp.MustCompile(`--` + name + `:\s*#([0-9a-fA-F]{6});`).FindStringSubmatch(block); m != nil {
			var rgb [3]float64
			for i := range rgb {
				v, err := strconv.ParseUint(m[1][2*i:2*i+2], 16, 8)
				require.NoError(t, err)
				rgb[i] = float64(v)
			}
			return rgb
		}
	}
	t.Fatalf("tokens.css has no --%s for the %s theme", name, theme)
	return [3]float64{}
}

func luminance(channel float64) float64 {
	c := channel / 255
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func relativeLuminance(rgb [3]float64) float64 {
	return 0.2126*luminance(rgb[0]) + 0.7152*luminance(rgb[1]) + 0.0722*luminance(rgb[2])
}

// contrast is the WCAG relative-luminance ratio of two colours.
func contrast(a, b [3]float64) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

var (
	borderRe = regexp.MustCompile(`\bborder-(mute|border)\b`)
	bgRe     = regexp.MustCompile(`\bbg-(bone|surface)\b`)
)

// assertFieldBorder fails when class draws a field border under 3:1 (WCAG 1.4.11) on its own fill, or on the surface
// card and bone page the field sits on, in either theme.
func assertFieldBorder(t *testing.T, class, where string) {
	t.Helper()
	b := borderRe.FindStringSubmatch(class)
	f := bgRe.FindStringSubmatch(class)
	require.NotNil(t, b, "%s has a token border: %s", where, class)
	require.NotNil(t, f, "%s has a token fill: %s", where, class)
	for _, theme := range []string{"light", "dark"} {
		for _, bg := range []string{f[1], "surface", "bone"} {
			assert.GreaterOrEqual(t, contrast(tokenValue(t, theme, b[1]), tokenValue(t, theme, bg)), 3.0, "%s: border-%s on %s in the %s theme", where, b[1], bg, theme)
		}
	}
}

// Every Input, Select and Textarea goes through atoms.fieldClass; its border is the only cue of the
// field's edge (#792, #796).
func TestAtomFields_BorderIs3To1(t *testing.T) {
	attrs := templ.Attributes{}
	for name, html := range map[string]string{
		"Input":    render(t, atoms.Input(atoms.FieldMd, "", attrs)),
		"Select":   render(t, atoms.Select(atoms.FieldSm, "w-full", attrs)),
		"Textarea": render(t, atoms.Textarea(atoms.FieldLg, "", attrs, "")),
	} {
		m := regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(html)
		require.NotNil(t, m, name)
		assertFieldBorder(t, m[1], name)
	}
}

// The Lit components render their own fields; each <input>, <select> and <textarea> carrying a border
// needs the same contrast.
func TestLitFields_BorderIs3To1(t *testing.T) {
	tag := regexp.MustCompile(`<(?:input|select|textarea)\b[^>]*?\bclass="([^"]*)"`)
	checked := 0
	require.NoError(t, filepath.WalkDir("../../../../static/src/components", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".wtest.ts") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range tag.FindAllStringSubmatch(string(src), -1) {
			if borderRe.MatchString(m[1]) {
				checked++
				assertFieldBorder(t, m[1], path)
			}
		}
		return nil
	}))
	assert.GreaterOrEqual(t, checked, 6, "the walk found the Lit fields (timeline-clip x5, take-comparator x1)")
}

func TestContrast_IsTheWCAGRatio(t *testing.T) {
	assert.InDelta(t, 21.0, contrast([3]float64{0, 0, 0}, [3]float64{255, 255, 255}), 0.01)
	assert.InDelta(t, 1.0, contrast([3]float64{139, 139, 142}, [3]float64{139, 139, 142}), 0.01)
}

// The border that is the cue of a field (--mute) is under 3:1 on no surface of either theme, and the lighter --border (a hairline for
// cards and dividers) is not a field border.
func TestTokens_MuteIsAFieldBorderInBothThemes(t *testing.T) {
	assertFieldBorder(t, "border-mute bg-surface", "mute on surface")
	assert.Less(t, contrast(tokenValue(t, "light", "border"), tokenValue(t, "light", "surface")), 3.0, "--border is a hairline, not a field edge")
}
