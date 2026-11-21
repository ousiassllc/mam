package refresh_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/refresh"
)

func render(t *testing.T, url, target, updatedAt string) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, refresh.Bar(url, target, updatedAt).Render(context.Background(), &sb))
	return sb.String()
}

// The button is the reader's own request: it reads the part named by target from url and swaps it whole, takes the place of a poll of the same
// part that is out (hx-sync replace), is named 更新 (the icon is decoration) and has a fixed id so that HTMX gives it the focus again after the swap.
func TestBar_IsANamedButtonThatSwapsItsTargetAndReplacesAPollOfIt(t *testing.T) {
	html := render(t, "/model-manager/models", "mm-catalog", "10:00:05")

	assert.Contains(t, html, `id="mm-catalog-refresh"`)
	assert.Contains(t, html, `hx-get="/model-manager/models"`)
	assert.Contains(t, html, `hx-target="#mm-catalog"`)
	assert.Contains(t, html, `hx-swap="outerHTML"`)
	assert.Contains(t, html, `hx-sync="#mm-catalog:replace"`)
	assert.Contains(t, html, `aria-label="更新"`)
	assert.Contains(t, html, `title="更新"`)
	assert.Contains(t, html, `aria-hidden="true"`, "the icon is decoration")
	assert.Contains(t, html, `group-[.htmx-request]:hidden`, "the icon gives way while the request runs")
	assert.Contains(t, html, `group-[.htmx-request]:inline-flex`, "the spinner shows only while the request runs")
	assert.Contains(t, html, `border-t-accent`, "the guide's spinner (atoms.Spinner, O12), not a turning icon")
	assert.NotContains(t, html, `group-[.htmx-request]:animate-spin`, "the icon itself does not turn")
	assert.NotContains(t, html, `motion-reduce:animate-none`, "that it is working is not a decoration: it turns for a reader who asked for less motion too")
}

// When it was read is small text beside the button; a part that has no single time (a list of cards that poll on their own) shows none.
func TestBar_ShowsTheTimeItWasReadOnlyWhenItHasOne(t *testing.T) {
	assert.Contains(t, render(t, "/x", "x", "10:00:05"), `最終更新 <time>10:00:05</time>`)
	assert.NotContains(t, render(t, "/x", "x", ""), "最終更新")
}

// The icon button is pressed like any button: it starts the ripple (the UI guide, M4).
func TestBar_ButtonIsMarkedForTheRipple(t *testing.T) {
	assert.Contains(t, render(t, "/model-manager/models", "mm-catalog", "10:00:05"), "data-ripple")
}
