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
)

func render(ctx context.Context, t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(ctx, &sb))
	return sb.String()
}

// The toast the client raises (toast.ts clones #toast-template) and the toast the server swaps in
// (atoms.ToastOOB) must be the same DOM: both come from atoms.Toast.
func TestAppLayout_ToastTemplateHoldsTheServerToastMarkup(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<main></main>"))
	page := render(ctx, t, layout.AppLayout("t", "csrf"))
	toast := render(context.Background(), t, atoms.Toast(""))

	assert.Contains(t, page, `<template id="toast-template">`+toast+`</template>`)
	assert.Contains(t, page, `id="toast-region"`)
}

// The "connection lost" notice is cloned by connection-notice.ts (the chat, the Shot card and the
// video player all show it): its markup is defined only in atoms.ConnectionNotice.
func TestAppLayout_ConnectionNoticeTemplateHoldsTheAtomMarkup(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<main></main>"))
	page := render(ctx, t, layout.AppLayout("t", "csrf"))
	notice := render(context.Background(), t, atoms.ConnectionNotice())

	assert.Contains(t, page, `<template id="connection-notice-template">`+notice+`</template>`)
	assert.Contains(t, notice, `data-action="reconnect"`)
	assert.Contains(t, notice, `data-action="reload"`)
	assert.Contains(t, notice, `role="alert"`)
}

// A page that several URLs show says which one is its own: the canonical link is in the head, and only when asked for.
func TestAppLayoutAt_CanonicalLinkIsInTheHeadOnlyWhenGiven(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<main></main>"))

	with := render(ctx, t, layout.AppLayoutAt("t", "csrf", "/settings/api-keys"))
	assert.Contains(t, with, `<link rel="canonical" href="/settings/api-keys">`)
	assert.Less(t, strings.Index(with, `rel="canonical"`), strings.Index(with, "</head>"))

	assert.NotContains(t, render(ctx, t, layout.AppLayout("t", "csrf")), `rel="canonical"`)
}
