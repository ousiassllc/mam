package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

func renderIcon(t *testing.T, name atoms.IconName, class string) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, atoms.Icon(name, class).Render(context.Background(), &sb))
	return sb.String()
}

// Every icon is a decoration: hidden from assistive technology, unfocusable, in the colour of the text around it, and
// it draws a shape of its own (a name without a case would draw nothing).
func TestIcon_EveryIconIsADecorativeSVGWithItsOwnShape(t *testing.T) {
	names := []atoms.IconName{
		atoms.IconFlask, atoms.IconNetwork, atoms.IconWorkflow, atoms.IconPackage,
		atoms.IconSettings, atoms.IconChart, atoms.IconReview, atoms.IconChevronRight,
		atoms.IconKey, atoms.IconText, atoms.IconMedia, atoms.IconLocal, atoms.IconPlug, atoms.IconUpdate, atoms.IconClose, atoms.IconCopy, atoms.IconHelp,
		atoms.IconImage, atoms.IconImagePlus, atoms.IconImageUp, atoms.IconImageOff,
	}
	shapes := map[string]atoms.IconName{}
	for _, name := range names {
		t.Run(string(name), func(t *testing.T) {
			html := renderIcon(t, name, "")

			assert.Contains(t, html, `aria-hidden="true"`)
			assert.Contains(t, html, `focusable="false"`)
			assert.Contains(t, html, `stroke="currentColor"`)
			assert.Contains(t, html, `fill="none"`)
			assert.NotContains(t, html, "<title")
			body := html[strings.Index(html, ">")+1 : strings.LastIndex(html, "</svg>")]
			require.NotEmpty(t, strings.TrimSpace(body), "the icon draws a shape")
			other, dup := shapes[body]
			assert.False(t, dup, "%s draws the same as %s", name, other)
			shapes[body] = name
		})
	}
}

func TestIcon_ClassIsTheSizeAndDefaultsToTwentyPixels(t *testing.T) {
	assert.Contains(t, renderIcon(t, atoms.IconChart, ""), `class="h-5 w-5 shrink-0"`)
	assert.Contains(t, renderIcon(t, atoms.IconChart, "h-4 w-4"), `class="h-4 w-4"`)
}

func TestIcon_UnknownNameDrawsNothing(t *testing.T) {
	assert.Empty(t, renderIcon(t, atoms.IconName("no-such-icon"), ""))
}
