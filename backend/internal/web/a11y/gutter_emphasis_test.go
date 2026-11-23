package a11y_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The chat screen's three bands (header, thread, composer) have the gutter of the other screens: 16 on a phone, 24 from md up (S5), so that their edges
// line up with the alert region above them.
func TestSources_ChatGutterIsSixteenThenTwentyFour(t *testing.T) {
	for _, file := range []string{"chat_workspace.templ", "chat_thread.templ", "chat_composer.templ"} {
		raw, err := os.ReadFile("../organisms/" + file)
		require.NoError(t, err)
		assert.Regexp(t, regexp.MustCompile(`\bpx-4\b[^"]*\bmd:px-6\b`), string(raw), "%s: the gutter is px-4 md:px-6", file)
	}
}

// The card that asks for a confirmation before a run is told by a 1px --mute border (S4: the emphasis border); border-accent is for what is selected
// or where the user is.
func TestSources_ConfirmCardsHaveTheEmphasisBorder(t *testing.T) {
	for _, tc := range []struct{ file, testID string }{
		{"../organisms/gentest/confirm.templ", `data-testid="gt-confirm"`},
		{"../organisms/gentest/chat/proposal.templ", `data-testid="gtc-proposal"`},
		{"../organisms/gentestinpaint/form.templ", `data-testid="gt-inpaint-confirm"`},
	} {
		raw, err := os.ReadFile(tc.file)
		require.NoError(t, err)
		assert.Contains(t, string(raw), tc.testID, tc.file)
		assert.Contains(t, string(raw), "rounded-lg border border-mute", "%s: the confirmation card has the emphasis border", tc.file)
		assert.NotContains(t, string(raw), "rounded-lg border border-accent", "%s: border-accent is for a selection", tc.file)
	}
}
