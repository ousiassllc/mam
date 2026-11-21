package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// The two buttons of the notice are the atoms' small buttons (the ripple of the UI guide, M4),
// and the scripts find them by data-action.
func TestConnectionNotice_ButtonsAreSmallButtonsWithTheirActions(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.ConnectionNotice().Render(context.Background(), &sb))
	html := sb.String()

	for _, action := range []string{"reconnect", "reload"} {
		assert.Contains(t, html, `data-action="`+action+`"`, action)
	}
	assert.Equal(t, 2, strings.Count(html, "data-ripple"))
}
