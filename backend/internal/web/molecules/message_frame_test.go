package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func TestMessageFrame_GivesTheClientTheURLsToRefetchWith(t *testing.T) {
	html := render(t, molecules.TextMessage(domain.ThreadMessage{ChatMessage: domain.ChatMessage{ID: "m?1#x y", ProjectID: "p/1", Kind: domain.KindText, Content: "hi"}}))

	assert.Contains(t, html, `data-card-url="/projects/p%2F1/messages/m%3F1%23x%20y"`)
	assert.Contains(t, html, `data-next-url="/projects/p%2F1/messages?after=m%3F1%23x+y"`)
}
