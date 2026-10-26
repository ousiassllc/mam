package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

func TestToastOOB_CarriesACloseButtonAndNothingThatDismissesItByItself(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.ToastOOB("予算を超えています").Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, `role="alert"`)
	assert.Contains(t, html, "予算を超えています")
	assert.Contains(t, html, `data-toast-close`)
	assert.Contains(t, html, `aria-label="閉じる"`)
	assert.NotContains(t, html, "hx-on", "htmx eval is off; the click is handled by toast.ts")
}

func TestToastOOB_WrapsTheSameToastMarkupInTheRegion(t *testing.T) {
	var toast, oob strings.Builder
	require.NoError(t, atoms.Toast("予算を超えています").Render(context.Background(), &toast))
	require.NoError(t, atoms.ToastOOB("予算を超えています").Render(context.Background(), &oob))

	assert.Contains(t, oob.String(), `id="toast-region"`)
	assert.Contains(t, oob.String(), `hx-swap-oob="beforeend"`)
	assert.Contains(t, oob.String(), toast.String())
}

func TestToast_EscapesTheMessage(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.Toast(`<img src=x onerror=alert(1)>`).Render(context.Background(), &sb))

	assert.NotContains(t, sb.String(), "<img")
	assert.Contains(t, sb.String(), `data-toast-message`)
}

func TestSuccessToastOOB_IsAStatusInTheRegionThatTheCloseButtonRemoves(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.SuccessToastOOB("API キーを保存しました").Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, `id="toast-region" hx-swap-oob="beforeend"`)
	assert.Contains(t, html, `role="status" data-toast`)
	assert.NotContains(t, html, `role="alert"`, "nothing is wrong")
	assert.Contains(t, html, "API キーを保存しました")
	assert.Contains(t, html, `data-toast-close`)
	assert.Contains(t, html, `data-toast-message`, "the same message is not shown twice (toast.ts drops a repeat)")
}
