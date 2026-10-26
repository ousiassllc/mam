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

	assert.Contains(t, html, `<dialog id="mam-modal" data-modal aria-labelledby="mam-modal-title"`)
	assert.NotContains(t, html, " open")
	assert.Contains(t, html, `<div id="mam-modal-body"></div>`)
	assert.Contains(t, html, `id="mam-modal-failure" role="alert"`)
	assert.Contains(t, html, `data-modal-close aria-label="閉じる"`)
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
