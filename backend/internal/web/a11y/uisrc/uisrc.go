// Package uisrc is what the checks of the a11y packages share: the UI sources they read (the templ, Go, TypeScript and CSS files that
// Tailwind reads for class names), the design tokens, a component rendered to a string and the splitting of a line into class-like words.
// It is a package of its own because the checks are over the line limit of one directory (.linterly.yml): a11y holds the checks of the
// colour, the shape and the names, a11y/texttest the ones of the text (its size, weight and colour).
package uisrc

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// Render renders c to a string.
func Render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// ReadTokens is static/src/css/tokens.css, the one place the values of the design tokens (colours, type, sizes) are written; the sources only name them.
func ReadTokens(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testenv.ModuleRoot(t), "static", "src", "css", "tokens.css"))
	require.NoError(t, err)
	return string(raw)
}

// ColorSources walks the sources Tailwind reads for class names: the Go and templ files of the web package and the
// TypeScript and CSS of the front end, without tests, generated *_templ.go, fixtures and the build output. The paths are
// relative to the working directory of the test (as `../organisms/sidenav/nav.templ` from a11y), sorted.
func ColorSources(t *testing.T) []string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	root := testenv.ModuleRoot(t)
	var files []string
	for dir, exts := range map[string][]string{
		filepath.Join(root, "backend", "internal", "web"): {".templ", ".go"},
		filepath.Join(root, "static", "src"):              {".ts", ".css"},
	} {
		require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
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
					rel, err := filepath.Rel(cwd, path)
					if err != nil {
						return err
					}
					files = append(files, filepath.ToSlash(rel))
				}
			}
			return nil
		}))
	}
	sort.Strings(files)
	return files
}

// Tokens splits a line into class-like words. A word ends at a space, a quote, a backtick, a brace, a comma,
// a parenthesis, a semicolon or an angle bracket, except inside [...] where an arbitrary variant such as
// has-[:checked]: or [&_a]: may hold any of them.
func Tokens(line string) []string {
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

// UtilityOf returns the class without its variants (hover:, has-[...]:, ...).
func UtilityOf(token string) string {
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
