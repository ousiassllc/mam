package a11y_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/a11y/uisrc"
)

// spec/components/color-rules.md is the source of the colour rules. colorAllowlist is its §3 table and
// colorExceptions its §4 table, class by class: TestColorRulesSpec_MirrorsTheTests fails when they differ.
// The colours are the tokens of the Ousiass UI guide (static/src/css/tokens.css, which is the one place a colour is written as a
// value): a class names a token (bg-surface, text-mute, border-border, ...), never a Tailwind palette shade, so a screen follows the
// Light / Dark theme by itself. A class a screen needs that is in neither list is mapped onto the table (same meaning, same class)
// or, when it carries a meaning the table has no row for, added to the exceptions with its reason in the spec.
var colorAllowlist = map[string][]string{
	"page":            {"bg-bone", "text-ink"},
	"toast":           {"bg-ink", "text-bone"},
	"card and modal":  {"bg-surface", "border-border"},
	"supporting text": {"text-mute"},
	"border":          {"border-border", "divide-border"},
	"raised face":     {"bg-accent-soft"},
	"hover surface":   {"hover:bg-accent-soft", "hover:border-mute", "peer-not-checked:hover:border-mute"},
	"hover text":      {"hover:text-ink"},
	"primary action":  {"bg-accent", "hover:bg-accent/85", "text-bone"},
	"link and icon":   {"text-accent", "decoration-mute"},
	"selected":        {"border-accent", "has-[:checked]:border-accent", "peer-checked:border-accent", "peer-checked:bg-accent-soft", "hover:border-accent", "border-t-accent", "border-accent-soft"},
	"danger":          {"text-danger", "border-danger/40", "bg-danger/10", "hover:bg-danger/10"},
	"warning":         {"text-warning", "border-warning/40", "bg-warning/10"},
	"success":         {"text-success", "border-success/40", "bg-success/10"},
	"info":            {"text-info", "border-info/40", "bg-info/10"},
}

var colorExceptions = map[string][]string{
	"field border":   {"border-mute"},
	"video backdrop": {"bg-black"},
	"reserved edge":  {"border-transparent"},
	"meter fill":     {"accent-danger", "accent-warning", "accent-success"},
	"overlay fill":   {"bg-danger/70", "bg-warning/70", "bg-success/70", "bg-info/70"},
}

func classSet(groups map[string][]string) map[string]bool {
	set := map[string]bool{}
	for _, classes := range groups {
		for _, c := range classes {
			set[c] = true
		}
	}
	return set
}

// colorUtility is a Tailwind utility that sets a colour: a palette shade, white / black / transparent /
// current, or an arbitrary value that is a colour. Sizes such as text-[10px] are not colours.
var colorUtility = regexp.MustCompile(`^(?:bg|text|border(?:-[xyselrtb])?|divide|ring|ring-offset|inset-ring|outline|fill|stroke|accent|caret|decoration|placeholder|shadow|inset-shadow|drop-shadow|from|via|to|selection|marker)-` +
	`(?:(?:neutral|slate|gray|zinc|stone|taupe|mauve|mist|olive|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}` +
	`|ink|bone|mute|surface|border|accent-soft|accent|focus|success|warning|danger|info` +
	`|white|black|transparent|current|inherit|\[(?:#|rgb|hsl|oklch|oklab|lab|lch|hwb|color:|var\()[^\]]*\])` +
	`(?:/\d+|/\[[^\]]*\])?$`)

// rawColor is a colour written outside a utility: a 6 or 8 digit hex (3 digits would be an issue number such as
// #865 in a comment), or a colour function.
var rawColor = regexp.MustCompile(`#[0-9a-fA-F]{6}(?:[0-9a-fA-F]{2})?\b|\b(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb|color-mix)\(`)

// tokensFile is the one source that writes colours as values: the design tokens of both themes.
const tokensFile = "tokens.css"

// Every colour utility of the UI sources is in the allowlist or the exceptions of spec/components/color-rules.md.
// A new class for a meaning the table already has is a change to the table, not to the screen (#867); a Tailwind palette
// shade (bg-neutral-900, text-indigo-300, ...) is never in it.
func TestSources_ColorUtilitiesAreInTheAllowlist(t *testing.T) {
	allowed, excepted := classSet(colorAllowlist), classSet(colorExceptions)
	files := uisrc.ColorSources(t)
	require.Greater(t, len(files), 100, "the walk found the sources")
	for _, path := range files {
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			for _, token := range uisrc.Tokens(line) {
				if colorUtility.MatchString(uisrc.UtilityOf(token)) && !allowed[token] && !excepted[token] {
					t.Errorf("%s:%d: %q is not in the colour allowlist of spec/components/color-rules.md: map it onto the table (same meaning, same class) or list it there as an exception with its reason", path, n+1, token)
				}
			}
			if m := rawColor.FindString(line); m != "" && filepath.Base(path) != tokensFile {
				t.Errorf("%s:%d: %q is a colour written outside a Tailwind palette class: use a class of spec/components/color-rules.md", path, n+1, m)
			}
		}
	}
}

// The checker rejects what it should: the helper is not a pass-through.
func TestColorUtility_RecognisesColoursAndNotSizes(t *testing.T) {
	for _, c := range []string{"bg-red-950/40", "text-amber-200", "border-t-indigo-400", "bg-[#123456]", "text-[rgb(1,2,3)]", "accent-red-400", "text-white", "bg-black", "border-transparent", "ring-offset-neutral-900", "text-neutral-500", "bg-zinc-900", "bg-surface", "text-mute", "border-border", "bg-accent-soft/60", "bg-danger/10", "accent-danger", "border-t-accent"} {
		assert.True(t, colorUtility.MatchString(c), c)
	}
	for _, c := range []string{"text-[10px]", "text-sm", "border-2", "border-l-2", "bg-clip-text", "rounded-md", "ring-2", "shadow-lg", "outline-hidden", "divide-y", "bg-linear-to-r", "font-semibold", "text-xl", "border-collapse"} {
		assert.False(t, colorUtility.MatchString(c), c)
	}
	assert.Equal(t, []string{"has-[:checked]:border-indigo-400", "text-red-200"}, uisrc.Tokens(`class={ "a", templ.KV("has-[:checked]:border-indigo-400 text-red-200", x) }`)[3:5])
	assert.Equal(t, "border-indigo-400", uisrc.UtilityOf("has-[:checked]:border-indigo-400"))
	assert.Equal(t, "bg-neutral-800", uisrc.UtilityOf("hover:bg-neutral-800"))
}

// specClasses returns the colour classes written in backticks between the begin and end markers of the spec: in
// the first column of each table row when firstColumn (the exceptions, whose reasons may name other classes), in
// the whole row otherwise (the allowlist, whose notes name none).
func specClasses(t *testing.T, spec, name string, firstColumn bool) map[string]bool {
	t.Helper()
	begin, end := "<!-- "+name+":begin -->", "<!-- "+name+":end -->"
	i, j := strings.Index(spec, begin), strings.Index(spec, end)
	require.True(t, i >= 0 && j > i, "spec/components/color-rules.md has %s and %s", begin, end)
	set := map[string]bool{}
	for _, row := range strings.Split(spec[i+len(begin):j], "\n") {
		if !strings.HasPrefix(row, "|") {
			continue
		}
		if firstColumn {
			row = strings.SplitN(row, "|", 3)[1]
		}
		for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(row, -1) {
			if colorUtility.MatchString(uisrc.UtilityOf(m[1])) {
				set[m[1]] = true
			}
		}
	}
	return set
}

// The tables in the spec and the lists of this test say the same, class by class.
func TestColorRulesSpec_MirrorsTheTests(t *testing.T) {
	raw, err := os.ReadFile("../../../../spec/components/color-rules.md")
	require.NoError(t, err)
	spec := string(raw)
	assert.Equal(t, classSet(colorAllowlist), specClasses(t, spec, "color-allowlist", false), "§3 of the spec against colorAllowlist")
	assert.Equal(t, classSet(colorExceptions), specClasses(t, spec, "color-exceptions", true), "§4 of the spec against colorExceptions")
	for c := range classSet(colorExceptions) {
		assert.False(t, classSet(colorAllowlist)[c], "%s is an exception and also in the allowlist", c)
	}
	palette := regexp.MustCompile(`-(?:neutral|slate|gray|zinc|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b|-white\b`)
	for _, set := range []map[string]bool{classSet(colorAllowlist), classSet(colorExceptions)} {
		for c := range set {
			assert.False(t, palette.MatchString(c), "%s is a Tailwind palette shade: the colours are the tokens of tokens.css", c)
		}
	}
}
