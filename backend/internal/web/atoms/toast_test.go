package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// The toast is a short line of what worked: a pill on --ink with --bone text, no close button (it goes by itself after 3 seconds), one icon and
// the words (the UI guide, O3).
func TestToast_IsAShortStatusPillOnInkWithoutAClose(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.Toast("保存しました").Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, `role="status" data-toast`)
	assert.Contains(t, html, "rounded-full")
	assert.Contains(t, html, "bg-ink")
	assert.Contains(t, html, "text-bone")
	assert.Contains(t, html, "min-h-10")
	assert.Contains(t, html, "保存しました")
	assert.Contains(t, html, "<svg", "the state is told by an icon as well as by the words")
	assert.NotContains(t, html, "data-toast-close", "it goes by itself")
	assert.NotContains(t, html, `role="alert"`, "nothing is wrong")
}

func TestToast_EscapesTheMessage(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.Toast(`<img src=x onerror=alert(1)>`).Render(context.Background(), &sb))

	assert.NotContains(t, sb.String(), "<img")
	assert.Contains(t, sb.String(), `data-toast-message`)
}

// A new toast replaces the one that is up: the OOB swap sets the content of the region, it does not append.
func TestToastOOB_ReplacesTheContentOfTheRegionWithTheSameToast(t *testing.T) {
	var toast, oob strings.Builder
	require.NoError(t, atoms.Toast("API キーを保存しました").Render(context.Background(), &toast))
	require.NoError(t, atoms.ToastOOB("API キーを保存しました").Render(context.Background(), &oob))

	assert.Contains(t, oob.String(), `id="toast-region" hx-swap-oob="innerHTML"`)
	assert.Contains(t, oob.String(), toast.String())
}
