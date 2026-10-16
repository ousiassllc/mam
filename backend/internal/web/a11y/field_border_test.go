package a11y_test

import (
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// neutral is the grey channel (0-255) of Tailwind 4's neutral scale (theme.css oklch, converted to sRGB).
var neutral = map[string]float64{"500": 0x73, "600": 0x52, "700": 0x40, "800": 0x26, "900": 0x17, "950": 0x0a}

func luminance(channel float64) float64 {
	c := channel / 255
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// contrast is the WCAG relative-luminance ratio of two greys.
func contrast(a, b float64) float64 {
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

var (
	borderRe = regexp.MustCompile(`\bborder-neutral-(\d+)\b`)
	bgRe     = regexp.MustCompile(`\bbg-neutral-(\d+)\b`)
)

// assertFieldBorder fails when class draws a field border under 3:1 (WCAG 1.4.11) on its own fill, or on
// the neutral-900 card and neutral-950 page the field sits on.
func assertFieldBorder(t *testing.T, class, where string) {
	t.Helper()
	b := borderRe.FindStringSubmatch(class)
	f := bgRe.FindStringSubmatch(class)
	require.NotNil(t, b, "%s has a neutral border: %s", where, class)
	require.NotNil(t, f, "%s has a neutral fill: %s", where, class)
	for _, bg := range []string{f[1], "900", "950"} {
		assert.GreaterOrEqual(t, contrast(neutral[b[1]], neutral[bg]), 3.0, "%s: border-neutral-%s on neutral-%s", where, b[1], bg)
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

func TestContrast_MatchesTheIssueNumbers(t *testing.T) {
	assert.InDelta(t, 4.18, contrast(neutral["500"], neutral["950"]), 0.01)
	assert.InDelta(t, 3.78, contrast(neutral["500"], neutral["900"]), 0.01)
	assert.InDelta(t, 2.53, contrast(neutral["600"], neutral["950"]), 0.01)
	assert.InDelta(t, 1.73, contrast(neutral["700"], neutral["900"]), 0.01)
}
