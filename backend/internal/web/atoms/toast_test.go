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
