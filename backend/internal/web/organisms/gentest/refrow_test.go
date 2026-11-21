package gentest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// The three buttons on a thumbnail (take it out, earlier, later) are icon buttons of the UI: a press gives the ripple (M4).
func TestRefRow_ThumbnailButtonsRipple(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, gentest.RefRow(gentest.Ref{Value: "library:p1:r1", Label: "顔", ThumbURL: "/t.png"}, "image", gentest.RefUI{}).Render(context.Background(), &sb))
	html := sb.String()

	for _, button := range []string{"data-gt-remove", `data-gt-move="-1"`, `data-gt-move="1"`} {
		assert.Contains(t, html, button+" data-ripple", button)
	}
}
