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
