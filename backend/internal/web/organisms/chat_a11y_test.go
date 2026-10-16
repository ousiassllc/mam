package organisms_test

import (
	"regexp"
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
