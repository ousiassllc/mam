package resulttile_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
)

func renderFavorite(t *testing.T, on bool) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, resulttile.FavoriteButton("r1", on).Render(context.Background(), &sb))
	return sb.String()
}

// The star stays the outline of Iconoir (I2): the state is told by the colour of the line (ink when on, mute when off) and by aria-pressed,
// never by a fill that the icon does not have.
func TestFavoriteButton_StarIsAnOutlineAndTheStateIsItsColourAndAriaPressed(t *testing.T) {
	on, off := renderFavorite(t, true), renderFavorite(t, false)

	assert.Contains(t, on, `aria-pressed="true"`)
	assert.Contains(t, on, "text-ink")
	assert.NotContains(t, on, "text-mute")
	assert.Contains(t, off, `aria-pressed="false"`)
	assert.Contains(t, off, "text-mute")
	assert.NotContains(t, off, "text-ink")
	for _, html := range []string{on, off} {
		assert.Contains(t, html, `fill="none"`)
		assert.NotContains(t, html, "fill-")
	}
}

// The favorite button is an icon button: a press gives the ripple (M4).
func TestFavoriteButton_Ripples(t *testing.T) {
	assert.Contains(t, renderFavorite(t, true), "data-ripple")
	assert.Contains(t, renderFavorite(t, false), "data-ripple")
}
