package layout_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/layout"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/sidenav"
	"github.com/ousiassllc/mam/backend/internal/web/themestate"
)

func render(ctx context.Context, t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(ctx, &sb))
	return sb.String()
}

// The toast the client raises (toast.ts clones #toast-template) and the toast the server swaps in
// (atoms.ToastOOB) must be the same DOM: both come from atoms.Toast. The same goes for the alert (#alert-template, atoms.ErrorAlertOOB)
// and the confirmation modal (#confirm-template, modal.Confirm).
func TestAppLayout_ToastTemplateHoldsTheServerToastMarkup(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<main></main>"))
	page := render(ctx, t, layout.AppLayout("t", "csrf", sidenav.At(sidenav.None)))
	toast := render(context.Background(), t, atoms.Toast(""))

	assert.Contains(t, page, `<template id="toast-template">`+toast+`</template>`)
	assert.Contains(t, page, `id="toast-region"`)

	alert := render(context.Background(), t, atoms.Alert(atoms.AlertError, atoms.ErrorAlertTitle, ""))
	assert.Contains(t, page, `<template id="alert-template">`+alert+`</template>`)
	assert.Contains(t, page, `<div id="alert-region"`)
	confirm := render(context.Background(), t, modal.Confirm())
	assert.Contains(t, page, `<template id="confirm-template">`+confirm+`</template>`)
}

// The "connection lost" notice is cloned by connection-notice.ts (the chat, the Shot card and the
// video player all show it): its markup is defined only in atoms.ConnectionNotice.
func TestAppLayout_ConnectionNoticeTemplateHoldsTheAtomMarkup(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<main></main>"))
	page := render(ctx, t, layout.AppLayout("t", "csrf", sidenav.At(sidenav.None)))
	notice := render(context.Background(), t, atoms.ConnectionNotice())

	assert.Contains(t, page, `<template id="connection-notice-template">`+notice+`</template>`)
	assert.Contains(t, notice, `data-action="reconnect"`)
	assert.Contains(t, notice, `data-action="reload"`)
	assert.Contains(t, notice, `role="alert"`)
}

// A page that several URLs show says which one is its own: the canonical link is in the head, and only when asked for.
func TestAppLayoutAt_CanonicalLinkIsInTheHeadOnlyWhenGiven(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<main></main>"))

	with := render(ctx, t, layout.AppLayoutAt("t", "csrf", "/settings/api-keys", sidenav.At(sidenav.None)))
	assert.Contains(t, with, `<link rel="canonical" href="/settings/api-keys">`)
	assert.Less(t, strings.Index(with, `rel="canonical"`), strings.Index(with, "</head>"))

	assert.NotContains(t, render(ctx, t, layout.AppLayout("t", "csrf", sidenav.At(sidenav.None))), `rel="canonical"`)
}

// The desktop window (Wails) is a web view: a swipe on a trackpad past the edge of the page stretches the whole
// screen like rubber and lets it spring back (#852). The page itself must not overscroll, on html and on body
// (Tailwind's overscroll-none = overscroll-behavior: none); the areas that scroll inside the page keep their own scrolling.
func TestAppLayout_PageDoesNotOverscroll(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<main></main>"))
	page := render(ctx, t, layout.AppLayout("t", "csrf", sidenav.At(sidenav.None)))

	assert.Regexp(t, `<html[^>]*class="[^"]*\boverscroll-none\b`, page)
	assert.Regexp(t, `<body[^>]*class="[^"]*\boverscroll-none\b`, page)
}

// The menu is part of the shell: it comes before the page (and after the skip link that jumps over it to the content), the page is
// in #content, and a page that says Off (the first-run guide) has neither the menu nor the skip link.
func TestAppLayout_HoldsTheSideMenuAndTheSkipLinkBeforeTheContent(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<main>PAGE</main>"))
	page := render(ctx, t, layout.AppLayout("t", "csrf", sidenav.At(sidenav.Settings)))

	skip, nav, content, body := strings.Index(page, `href="#content"`), strings.Index(page, `<nav id="sidenav"`), strings.Index(page, `id="content"`), strings.Index(page, "PAGE")
	assert.Less(t, skip, nav)
	assert.Less(t, nav, content)
	assert.Less(t, content, body)
	assert.Contains(t, page, `data-key="settings" aria-current="page"`)

	off := render(ctx, t, layout.AppLayout("t", "csrf", sidenav.Off()))
	assert.NotContains(t, off, `<nav id="sidenav"`)
	assert.NotContains(t, off, `href="#content"`)
	assert.Contains(t, off, "PAGE")
}

// The theme the user chose is on <html> from the first byte, so the page is not drawn in the other theme first; without a choice there is no
// data-theme and tokens.css follows the system.
func TestAppLayout_HTMLCarriesTheChosenTheme(t *testing.T) {
	children := templ.WithChildren(context.Background(), templ.Raw("<main></main>"))
	for _, theme := range []string{themestate.Dark, themestate.Light} {
		page := render(themestate.With(children, theme), t, layout.AppLayout("t", "csrf", sidenav.At(sidenav.None)))
		assert.Contains(t, page, `data-theme="`+theme+`"`)
	}
	page := render(children, t, layout.AppLayout("t", "csrf", sidenav.At(sidenav.None)))
	assert.NotContains(t, page, "data-theme=", "no choice: follow the system")
}
