package web_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

var (
	buttonTag    = regexp.MustCompile(`<button([^>]*)>`)
	disabledElt  = regexp.MustCompile(`hx-disabled-elt="([^"]*)"`)
	attrSelector = regexp.MustCompile(`\[(\w+)=([^\]]+)\]`)
)

func hasFlag(attrs, name string) bool {
	return regexp.MustCompile(`(^|\s)` + name + `(\s|=|$)`).MatchString(attrs)
}

// An approval card starts its form with a disabled, hidden submit button so that Enter in the comment field
// submits nothing (#281). htmx's `find X` is `querySelector`: it disables the first match only, so
// `find button[type=submit]` named that placeholder and left "承認" and "修正を依頼" pressable while the
// answer was on its way (#811). This reads the rendered card and resolves each `find` of hx-disabled-elt the
// way htmx does (the first button with the given attributes): it must be a visible, enabled button, and
// every visible submit button of the form must be one of them.
func TestApprovalCard_DisablesItsVisibleSubmitButtonsWhileTheAnswerIsOnItsWay(t *testing.T) {
	html := renderHTML(t, molecules.ApprovalCard(domain.ThreadMessage{ChatMessage: domain.ChatMessage{
		ID: "a1", Kind: domain.KindApproval, Content: "世界観 v2 を反映する", ApprovalStatus: domain.ApprovalPending,
		RefType: domain.RefNotePromotion,
	}}))
	elt := disabledElt.FindStringSubmatch(html)
	require.NotNil(t, elt, "the approval form has no hx-disabled-elt")
	buttons := buttonTag.FindAllStringSubmatch(html, -1)
	require.NotEmpty(t, buttons)

	targeted := map[string]bool{}
	for _, part := range strings.Split(elt[1], ",") {
		selector, ok := strings.CutPrefix(strings.TrimSpace(part), "find button")
		require.True(t, ok, "hx-disabled-elt %q: only `find button[attr=value]` is resolved by this test", part)
		var found string
		for _, b := range buttons {
			matches := true
			for _, a := range attrSelector.FindAllStringSubmatch(selector, -1) {
				matches = matches && strings.Contains(b[1], a[1]+`="`+a[2]+`"`)
			}
			if matches {
				found = b[1]
				break
			}
		}
		require.NotEmpty(t, found, "hx-disabled-elt %q matches no button", part)
		assert.False(t, hasFlag(found, "disabled") || hasFlag(found, "hidden"),
			"hx-disabled-elt %q resolves to the hidden placeholder, which disables nothing", part)
		targeted[found] = true
	}

	for _, b := range buttons {
		if strings.Contains(b[1], `type="submit"`) && !hasFlag(b[1], "hidden") {
			assert.True(t, targeted[b[1]], "the visible submit button <button%s> stays enabled while the request is in flight", b[1])
		}
	}
}
