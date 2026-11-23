package desktop

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boneOf returns the --bone of the block of tokens.css that opens with opener.
func boneOf(t *testing.T, opener string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../static/src/css/tokens.css")
	require.NoError(t, err)
	css := string(raw)
	start := strings.Index(css, opener)
	require.GreaterOrEqual(t, start, 0, "tokens.css has %q", opener)
	block := css[start:]
	block = block[:strings.Index(block, "}")]
	m := regexp.MustCompile(`--bone:\s*(#[0-9a-fA-F]{6});`).FindStringSubmatch(block)
	require.NotNil(t, m, "%q has --bone", opener)
	return strings.ToLower(m[1])
}

// The waiting page and the window take their colours from --bone of tokens.css, so that a Light user does not see a dark screen
// before the app (C5, #1204).
func TestWaitingPage_FollowsTheBoneOfBothThemes(t *testing.T) {
	light := boneOf(t, ":root {")
	dark := boneOf(t, `:root[data-theme="dark"] {`)
	assert.Contains(t, WaitingPage, "body{margin:0;background:"+light+"}", "Light is the default")
	assert.Contains(t, WaitingPage, "@media (prefers-color-scheme:dark){body{background:"+dark+"}}", "Dark follows the system")
}

func TestWindowBackground_IsTheLightBone(t *testing.T) {
	got := fmt.Sprintf("#%02x%02x%02x", WindowBackground[0], WindowBackground[1], WindowBackground[2])
	assert.Equal(t, boneOf(t, ":root {"), got)
}
