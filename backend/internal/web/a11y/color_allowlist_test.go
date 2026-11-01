package a11y_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec/components/color-rules.md is the source of the colour rules. colorAllowlist is its §3 table and
// colorExceptions its §4 table, class by class: TestColorRulesSpec_MirrorsTheTests fails when they differ.
// A class a screen needs that is in neither is mapped onto the table (same meaning, same class) or, when it
// carries a meaning the table has no row for, added to the exceptions with its reason in the spec (#867).
var colorAllowlist = map[string][]string{
	"page":            {"bg-neutral-950", "text-neutral-100"},
	"card and modal":  {"bg-neutral-900", "border-neutral-800", "border-neutral-700"},
	"supporting text": {"text-neutral-400"},
	"weaker body":     {"text-neutral-300", "text-neutral-200"},
	"border":          {"border-neutral-800", "border-neutral-700", "divide-neutral-800"},
	"hover surface":   {"hover:bg-neutral-800", "hover:border-neutral-600", "hover:bg-neutral-800/60"},
	"focus ring":      {"focus-visible:ring-indigo-400", "peer-focus-visible:ring-indigo-400", "focus-visible:ring-offset-neutral-950", "focus-visible:ring-offset-neutral-900"},
	"primary action":  {"bg-indigo-500", "hover:bg-indigo-400", "text-white"},
	"link and icon":   {"text-indigo-300"},
	"danger":          {"text-red-300", "border-red-500/60", "bg-red-500/10", "bg-red-500/20", "hover:bg-red-500/10"},
	"warning":         {"text-amber-300", "border-amber-500/60", "bg-amber-500/10", "bg-amber-500/20"},
	"success":         {"text-emerald-300", "border-emerald-500/60", "bg-emerald-500/10", "bg-emerald-500/20"},
	"info":            {"text-sky-300", "border-sky-500/60", "bg-sky-500/10", "bg-sky-500/20"},
	"neutral badge":   {"bg-neutral-700", "text-neutral-300"},
	"quiet action":    {"border-neutral-600", "text-neutral-200", "text-neutral-400"},
	"modal backdrop":  {"backdrop:bg-black/70"},
}

var colorExceptions = map[string][]string{
	"field border":   {"border-neutral-500"},
	"raised face":    {"bg-neutral-800"},
	"hover text":     {"hover:text-neutral-100"},
	"selected":       {"border-indigo-400", "has-[:checked]:border-indigo-400", "bg-indigo-500/20", "border-t-indigo-400", "peer-checked:border-indigo-400", "peer-checked:bg-indigo-500/20", "peer-checked:shadow-indigo-400/40"},
	"video backdrop": {"bg-black"},
	"reserved edge":  {"border-transparent"},
	"meter fill":     {"accent-red-400", "accent-amber-400", "accent-emerald-400"},
	"overlay fill":   {"bg-red-500/70", "bg-amber-500/70", "bg-emerald-500/70", "bg-sky-500/70"},
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
	`|white|black|transparent|current|inherit|\[(?:#|rgb|hsl|oklch|oklab|lab|lch|hwb|color:|var\()[^\]]*\])` +
	`(?:/\d+|/\[[^\]]*\])?$`)

// rawColor is a colour written outside a utility: a 6 or 8 digit hex (3 digits would be an issue number such as
// #865 in a comment), or a colour function.
var rawColor = regexp.MustCompile(`#[0-9a-fA-F]{6}(?:[0-9a-fA-F]{2})?\b|\b(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb|color-mix)\(`)

// tokens splits a line into class-like words. A word ends at a space, a quote, a backtick, a brace, a comma,
// a parenthesis, a semicolon or an angle bracket, except inside [...] where an arbitrary variant such as
// has-[:checked]: or [&_a]: may hold any of them.
func tokens(line string) []string {
	var out []string
	depth, start := 0, -1
	flush := func(end int) {
		if start >= 0 {
			out = append(out, strings.Trim(line[start:end], ".,:;!"))
			start = -1
		}
	}
	for i, r := range line {
		switch {
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		}
		if depth == 0 && strings.ContainsRune(" \t\"'`{}(),;<>=$|", r) {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(line))
	return out
}

// utilityOf returns the class without its variants (hover:, has-[...]:, ...).
func utilityOf(token string) string {
	depth, cut := 0, 0
	for i, r := range token {
		switch {
		case r == '[':
			depth++
		case r == ']':
			depth--
		case r == ':' && depth == 0:
			cut = i + 1
		}
	}
	return strings.TrimPrefix(token[cut:], "!")
}

// colorSources walks the sources Tailwind reads for class names: the Go and templ files of the web package and the
// TypeScript and CSS of the front end, without tests, generated *_templ.go, fixtures and the build output.
func colorSources(t *testing.T) []string {
	t.Helper()
	var files []string
	for root, exts := range map[string][]string{
		"..":                     {".templ", ".go"},
		"../../../../static/src": {".ts", ".css"},
	} {
		require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == "dist" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			skip := strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_templ.go") ||
				strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".wtest.ts")
			for _, ext := range exts {
				if !skip && strings.HasSuffix(name, ext) {
					files = append(files, path)
				}
			}
			return nil
		}))
	}
	sort.Strings(files)
	return files
}

// Every colour utility of the UI sources is in the allowlist or the exceptions of spec/components/color-rules.md.
// A new shade or /opacity for a meaning the table already has is a change to the table, not to the screen (#867).
func TestSources_ColorUtilitiesAreInTheAllowlist(t *testing.T) {
	allowed, excepted := classSet(colorAllowlist), classSet(colorExceptions)
	files := colorSources(t)
	require.Greater(t, len(files), 100, "the walk found the sources")
	for _, path := range files {
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for n, line := range strings.Split(string(src), "\n") {
			for _, token := range tokens(line) {
				if colorUtility.MatchString(utilityOf(token)) && !allowed[token] && !excepted[token] {
					t.Errorf("%s:%d: %q is not in the colour allowlist of spec/components/color-rules.md: map it onto the table (same meaning, same class) or list it there as an exception with its reason", path, n+1, token)
				}
			}
			if m := rawColor.FindString(line); m != "" {
				t.Errorf("%s:%d: %q is a colour written outside a Tailwind palette class: use a class of spec/components/color-rules.md", path, n+1, m)
			}
		}
	}
}

// The checker rejects what it should: the helper is not a pass-through.
func TestColorUtility_RecognisesColoursAndNotSizes(t *testing.T) {
	for _, c := range []string{"bg-red-950/40", "text-amber-200", "border-t-indigo-400", "bg-[#123456]", "text-[rgb(1,2,3)]", "accent-red-400", "text-white", "bg-black", "border-transparent", "ring-offset-neutral-900", "text-neutral-500", "bg-zinc-900"} {
		assert.True(t, colorUtility.MatchString(c), c)
	}
	for _, c := range []string{"text-[10px]", "text-sm", "border-2", "border-l-2", "bg-clip-text", "rounded-md", "ring-2", "shadow-lg", "outline-hidden", "divide-y", "bg-linear-to-r"} {
		assert.False(t, colorUtility.MatchString(c), c)
	}
	assert.Equal(t, []string{"has-[:checked]:border-indigo-400", "text-red-200"}, tokens(`class={ "a", templ.KV("has-[:checked]:border-indigo-400 text-red-200", x) }`)[3:5])
	assert.Equal(t, "border-indigo-400", utilityOf("has-[:checked]:border-indigo-400"))
	assert.Equal(t, "bg-neutral-800", utilityOf("hover:bg-neutral-800"))
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
			if colorUtility.MatchString(utilityOf(m[1])) {
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
	assert.False(t, classSet(colorAllowlist)["text-neutral-500"] || classSet(colorExceptions)["text-neutral-500"], "text-neutral-500 stays banned (contrast_test.go)")
}
