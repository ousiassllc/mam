package a11y_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// The Chat Workspace is the page's only body, so it is the main landmark (#778).
func TestChatWorkspace_RootIsTheMainLandmark(t *testing.T) {
	html := renderHTML(t, organisms.ChatWorkspace(domain.Project{ID: "p1"}, nil))

	root := regexp.MustCompile(`^\s*<main[^>]*>`).FindString(html)
	require.NotEmpty(t, root, html)
	assert.Contains(t, root, `id="chat-workspace"`)
	assert.Contains(t, html, "</main>")
}

// A placeholder is not an accessible name: it disappears on input (#779).
func TestChatComposer_MessageInputHasAnAccessibleName(t *testing.T) {
	html := renderHTML(t, organisms.ChatComposer(domain.Project{ID: "p1"}))

	textarea := regexp.MustCompile(`<textarea[^>]*name="content"[^>]*>`).FindString(html)
	require.NotEmpty(t, textarea, html)
	assert.Contains(t, textarea, `aria-label="メッセージ"`)
}

// The label "＋" is only a glyph: the file input is named for what it does (#780), and, being sr-only, its
// keyboard focus shows on the label that follows it (peer).
func TestChatComposer_AttachmentInputIsNamedAndItsFocusIsVisible(t *testing.T) {
	html := renderHTML(t, organisms.ChatComposer(domain.Project{ID: "p1"}))

	input := regexp.MustCompile(`<input[^>]*name="attachments"[^>]*>`).FindString(html)
	require.NotEmpty(t, input, html)
	assert.Contains(t, input, `aria-label="画像・動画を添付"`)

	label := regexp.MustCompile(`<label[^>]*for="composer-files"[^>]*>`).FindString(html)
	require.NotEmpty(t, label, html)
	assert.Contains(t, label, "peer-focus-visible:ring-2")
	// peer-* styles the sibling that follows the peer, so the input must come first.
	assert.Regexp(t, `class="[^"]*\bpeer\b`, input)
	assert.Less(t, strings.Index(html, input), strings.Index(html, label))
}
