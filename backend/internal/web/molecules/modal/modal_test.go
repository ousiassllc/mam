package modal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// The host is a native dialog that is named by the heading of whatever fills it, is empty and closed when drawn, and has
// the close button the script wires; its body is the target the buttons that open it fill.
func TestHost_IsAClosedEmptyNativeDialogWithACloseButton(t *testing.T) {
	html := render(t, modal.Host())

	assert.Contains(t, html, `<dialog id="mam-modal" data-modal aria-modal="true" aria-labelledby="mam-modal-title"`)
	assert.NotContains(t, html, " open")
	assert.Contains(t, html, `<div id="mam-modal-body"></div>`)
	assert.Contains(t, html, `id="mam-modal-failure" role="alert"`)
	assert.Contains(t, html, `data-modal-close data-ripple aria-label="閉じる"`)
	assert.NotContains(t, html, "text-neutral-500")
}

// A button that opens the modal asks for its body, puts it in the dialog's body, and has a key that survives the
// replacement of its row: what the script returns the focus by.
func TestOpenAttrs_FillTheBodyAndCarryTheFocusKey(t *testing.T) {
	attrs := modal.OpenAttrs("/settings/credentials/kling/edit", "credential-kling")

	assert.Equal(t, "/settings/credentials/kling/edit", attrs["hx-get"])
	assert.Equal(t, "#mam-modal-body", attrs["hx-target"])
	assert.Equal(t, "innerHTML", attrs["hx-swap"])
	assert.Equal(t, "credential-kling", attrs["data-focus-key"])
	assert.Equal(t, "this:drop", attrs["hx-sync"], "a double click must not send the request twice: the second answer would replace what was typed")
	assert.Equal(t, "button", attrs["type"], "it never submits a form it is in")
	assert.Contains(t, attrs, "data-modal-open")
}

// A link that opens the modal keeps its address (the page of the same content) and asks for the body like the button does.
func TestOpenLinkAttrs_KeepTheAddressAndAskForTheBody(t *testing.T) {
	attrs := modal.OpenLinkAttrs("/generation-test/results/r1", "gt-result-r1")

	assert.Equal(t, "/generation-test/results/r1", attrs["href"], "a string: a spread attribute that is a templ.SafeURL is not written at all")
	assert.Equal(t, "/generation-test/results/r1", attrs["hx-get"])
	assert.Equal(t, "#mam-modal-body", attrs["hx-target"])
	assert.Equal(t, "gt-result-r1", attrs["data-focus-key"])
	assert.Contains(t, attrs, "data-modal-open")
	assert.NotContains(t, attrs, "type", "a link is not a button")
}

// The host is as wide as a body that asks for it (the result of the generation test: the picture and its settings side by side).
func TestHost_IsWideWhileTheBodyAsksForIt(t *testing.T) {
	assert.Contains(t, render(t, modal.Host()), "has-[["+modal.WideAttr+"]]:w-[min(64rem")
}

// The dialog is what scrolls, and the head must stay in reach while a long body scrolls under it (Issue #1097): the close button is the first child in a
// sticky box that takes no room, so that it is neither below the body nor scrolled away.
func TestHost_KeepsTheCloseButtonStuckAtTheTopOfTheScrollingDialog(t *testing.T) {
	html := render(t, modal.Host())

	assert.Contains(t, html, "overflow-y-auto", "the dialog is the scroller")
	sticky := strings.Index(html, `class="sticky top-0 z-sticky h-0"`)
	require.GreaterOrEqual(t, sticky, 0, "the close button sits in a zero-height sticky box")
	assert.Less(t, sticky, strings.Index(html, `data-modal-close`), "the button is inside the sticky box")
	assert.Less(t, strings.Index(html, `data-modal-close`), strings.Index(html, `id="mam-modal-body"`), "the button comes before the body")
}

// The name of the dialog sticks to the top of the scrolling dialog, so that the title is still seen when the body is long (Issue #1097).
func TestFrame_KeepsItsTitleStuckAtTheTopOfTheScrollingDialog(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, modal.Frame("結果", "").Render(templ.WithChildren(context.Background(), templ.Raw("<p>本文</p>")), &sb))
	html := sb.String()

	h2 := html[strings.Index(html, `<h2 id="mam-modal-title"`):]
	h2 = h2[:strings.Index(h2, ">")]
	assert.Contains(t, h2, "sticky top-0")
	assert.Contains(t, h2, "bg-surface", "the body scrolls under an opaque title")
	assert.Less(t, strings.Index(html, "結果"), strings.Index(html, "<p>本文</p>"))
}

// The frame names the dialog (an h2 with the id the dialog is labelled by) and describes it only when asked to.
func TestFrame_IsNamedByItsHeadingAndDescribedOnlyWhenItHasADescription(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<form>F</form>"))
	frame := func(desc string) string {
		var sb strings.Builder
		require.NoError(t, modal.Frame("Kling の API キー", desc).Render(ctx, &sb))
		return sb.String()
	}

	with := frame("説明")
	assert.Contains(t, with, `<h2 id="mam-modal-title"`)
	assert.Contains(t, with, `id="mam-modal-desc"`)
	assert.Less(t, strings.Index(with, "Kling の API キー"), strings.Index(with, "<form>F</form>"))
	assert.NotContains(t, frame(""), "mam-modal-desc")
}

func TestError_IsAnAlertTheScriptCanFocus(t *testing.T) {
	html := render(t, modal.Error("入力が正しくありません"))

	assert.Contains(t, html, `id="mam-modal-error" role="alert" tabindex="-1"`)
	assert.Contains(t, html, "入力が正しくありません")
}

// A body that only says why there is nothing to edit has the reason and a way to close, focused first.
func TestNotice_HasTheReasonAndACloseButtonThatTakesTheFocus(t *testing.T) {
	html := render(t, modal.Notice("API キー", "対象が見つかりません"))

	assert.Contains(t, html, `role="alert"`)
	assert.Contains(t, html, "対象が見つかりません")
	assert.Contains(t, html, `data-modal-close`)
	assert.Contains(t, html, `autofocus`)
	assert.NotContains(t, html, "<form")
}
