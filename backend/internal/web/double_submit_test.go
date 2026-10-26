package web_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	hxPostAttr    = regexp.MustCompile(`\shx-(post|put)[=\s]`)
	hxPostOnly    = regexp.MustCompile(`\shx-post[=\s]`)
	hxAutoTrigger = regexp.MustCompile(`hx-trigger="change[^"]*"`)
	formIDAttr    = regexp.MustCompile(`\sid="([^"]+)"`)
	noDisabledElt = map[string]string{"chat-composer": "the composer stays editable while a message is on its way"}
	// The PUT forms of the screens that came before the modal dialog: they save the same values in place, so a repeat
	// is the same PUT again (idempotent) and cannot make a second row. A form of any other file has the guards, or
	// is added here with its reason.
	idempotentPutForms = map[string]string{
		"molecules/lock_toggle.templ":        "a toggle: the same lock is set again",
		"molecules/note_card.templ":          "an edit of one note: the same text is saved again",
		"organisms/branch_view.templ":        "an edit of one branch: the same values are saved again",
		"organisms/brief_form.templ":         "the brief: the same text is saved again",
		"organisms/policy_form.templ":        "the quality policy: the same values are saved again",
		"organisms/review_workspace.templ":   "the decision of a review: the same decision is saved again",
		"organisms/seriesview/view.templ":    "the series: the same values are saved again",
		"organisms/shotinspector/view.templ": "the shot spec: the same values are saved again",
		"organisms/worldstate/view.templ":    "the world state: the same values are saved again",
	}
	requiredGuards = []string{`hx-sync="this:drop"`, `hx-disabled-elt=`}
)

// A form that posts shows nothing until the server answers, so a second click or Enter would queue a
// second request (htmx's default) and run it after the first: a second Scene, Shot, note, reference,
// constraint, Review request or comment, or a false failure toast when the server refuses the repeat
// (#651, #669, #676, #684). Every <form hx-post> of the templates therefore drops a request while one
// is in flight (hx-sync="this:drop") and disables its submit button (hx-disabled-elt). The check reads
// the .templ sources, so that a form added later cannot skip the guard unnoticed. The forms that save
// on every change (hx-trigger="change") are idempotent PUTs and are not covered. The forms of the modal dialog save
// with hx-put and are covered like hx-post: a second click on "保存" must not queue a second save behind the first
// (only the hx-put forms of idempotentPutForms are exempt).
func TestPostingForms_IgnoreASecondSubmitWhileOneIsInFlight(t *testing.T) {
	sources := sourcesUnder(t, ".templ")
	require.NotEmpty(t, sources)
	seen := 0
	for _, f := range sources {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		src := string(raw)
		for _, m := range formOpening.FindAllStringIndex(src, -1) {
			tag := src[m[0]:untilClosed(src, m[0])]
			if !hxPostAttr.MatchString(tag) || hxAutoTrigger.MatchString(tag) {
				continue
			}
			if !hxPostOnly.MatchString(tag) && idempotentPutForms[f] != "" {
				continue
			}
			seen++
			where := f + ":" + lineOf(src, m[0])
			for _, guard := range requiredGuards {
				if guard == `hx-disabled-elt=` {
					if id := formIDAttr.FindStringSubmatch(tag); id != nil && noDisabledElt[id[1]] != "" {
						continue
					}
				}
				assert.Contains(t, tag, guard, "%s: a form that posts does not guard against a second submit", where)
			}
		}
	}
	assert.Positive(t, seen, "no posting form found: the pattern no longer matches the templates")
}

func lineOf(src string, pos int) string {
	return strconv.Itoa(1 + strings.Count(src[:pos], "\n"))
}
