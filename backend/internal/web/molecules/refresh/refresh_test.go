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
	assert.Contains(t, html, `motion-reduce:animate-none`, "the icon does not turn for a reader who asked for less motion")
}

// When it was read is small text beside the button; a part that has no single time (a list of cards that poll on their own) shows none.
func TestBar_ShowsTheTimeItWasReadOnlyWhenItHasOne(t *testing.T) {
	assert.Contains(t, render(t, "/x", "x", "10:00:05"), `最終更新 <time>10:00:05</time>`)
	assert.NotContains(t, render(t, "/x", "x", ""), "最終更新")
}
