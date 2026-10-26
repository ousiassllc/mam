package web_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A destructive control asks before it acts (#539): every hx-delete and every ToneDanger (red)
// button in the templates carries hx-confirm, on itself or on the form it submits. The check reads
// the .templ sources, so that a button added later cannot skip the dialog unnoticed.
func TestDestructiveControls_AskForConfirmation(t *testing.T) {
	sources := sourcesUnder(t, ".templ")
	require.NotEmpty(t, sources)
	seen := 0
	for _, f := range sources {
		if strings.HasPrefix(f, "atoms/") {
			continue // the atoms define the controls; the callers decide what they do
		}
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		src := string(raw)
		exempt := confirmExempt[f]
		for _, m := range destructiveMarker.FindAllStringIndex(src, -1) {
			seen++
			if exempt.reason != "" && exempt.count > 0 {
				exempt.count--
				continue
			}
			assert.Contains(t, controlOf(src, m[0]), "hx-confirm", "%s:%d is destructive but does not ask first", f, 1+strings.Count(src[:m[0]], "\n"))
		}
	}
	assert.Positive(t, seen, "no destructive control found: the pattern no longer matches the templates")
}

// confirmExempt are the controls that undo a change inside a modal without asking: the owner decided that the button
// "この Agent を既定に戻す" of the LLM modal has no confirmation (spec/components/settings-screen-categories.md §7 question 3):
// the modal is open, the change is one Agent's and it can be made again. count is how many controls of the file are exempt.
var confirmExempt = map[string]struct {
	reason string
	count  int
}{
	"organisms/settings/llm/modal.templ": {reason: "settings-screen-categories.md §2 and §7 question 3", count: 1},
}

var (
	destructiveMarker = regexp.MustCompile(`hx-delete|ToneDanger`)
	formOpening       = regexp.MustCompile(`<form[\s>]`)
)

// controlOf returns the source of the control the marker at pos belongs to: the enclosing
// templ.Attributes literal or tag, plus the opening tag of the enclosing form (the confirmation of
// a submit button can sit on its form).
func controlOf(src string, pos int) string {
	start := strings.LastIndex(src[:pos], "templ.Attributes{")
	if tag := strings.LastIndex(src[:pos], "<"); tag > start {
		start = tag
	}
	if call := strings.LastIndex(src[:pos], "@atoms."); call > start {
		start = call
	}
	if start < 0 {
		start = pos
	}
	control := src[start:untilClosed(src, start)]
	if opening := formOpening.FindAllStringIndex(src[:pos], -1); len(opening) > 0 {
		form := src[opening[len(opening)-1][0]:]
		if !strings.Contains(form[:pos-opening[len(opening)-1][0]], "</form>") { // still inside it
			control += form[:strings.Index(form, ">")+1]
		}
	}
	return control
}

// untilClosed is the end of the statement that starts at start: a tag ends at its ">", a call or a
// literal where its brackets close ({…} and (…) inside are skipped).
func untilClosed(src string, start int) int {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{', '(':
			depth++
		case '}', ')':
			depth--
			if depth <= 0 && src[start] != '<' {
				return i + 1
			}
		case '>':
			if depth <= 0 && src[start] == '<' {
				return i + 1
			}
		}
	}
	return len(src)
}
