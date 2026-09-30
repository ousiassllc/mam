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
