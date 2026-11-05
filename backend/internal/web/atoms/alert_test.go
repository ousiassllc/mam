package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// The four alerts of the UI guide each say their state with an icon, a heading and a colour, never with the colour alone (O3, C4).
func TestAlert_EachKindHasItsIconHeadingAndRole(t *testing.T) {
	for kind, want := range map[atoms.AlertKind]struct{ role, tone string }{
		atoms.AlertInfo:    {"status", "info"},
		atoms.AlertSuccess: {"status", "success"},
		atoms.AlertWarning: {"alert", "warning"},
		atoms.AlertError:   {"alert", "danger"},
	} {
		var sb strings.Builder
		require.NoError(t, atoms.Alert(kind, "見出し", "本文です").Render(context.Background(), &sb))
		html := sb.String()

		assert.Contains(t, html, `role="`+want.role+`" data-alert data-kind="`+string(kind)+`"`, kind)
		assert.Contains(t, html, "border-"+want.tone+"/60 bg-"+want.tone+"/10", kind)
		assert.Contains(t, html, "<svg", "%s: an icon", kind)
		assert.Contains(t, html, "見出し", kind)
		assert.Contains(t, html, `data-alert-message>本文です`, kind)
		assert.Contains(t, html, `data-alert-close`, "%s: it stays until it is closed", kind)
	}
}

func TestAlert_EscapesItsText(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.Alert(atoms.AlertError, "x", `<img src=x onerror=alert(1)>`).Render(context.Background(), &sb))

	assert.NotContains(t, sb.String(), "<img")
}

// What went wrong is an alert at the top of the page, appended (a second failure does not wipe the first), with the same markup as the one the
// client raises from #alert-template.
func TestErrorAlertOOB_AppendsTheErrorAlertToTheRegion(t *testing.T) {
	var alert, oob strings.Builder
	require.NoError(t, atoms.Alert(atoms.AlertError, atoms.ErrorAlertTitle, "予算を超えています").Render(context.Background(), &alert))
	require.NoError(t, atoms.ErrorAlertOOB("予算を超えています").Render(context.Background(), &oob))

	assert.Contains(t, oob.String(), `id="alert-region" hx-swap-oob="beforeend"`)
	assert.Contains(t, oob.String(), alert.String())
	assert.Contains(t, oob.String(), `role="alert"`)
}
