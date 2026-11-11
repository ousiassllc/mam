package secretform_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func card(open bool) domain.ThreadMessage {
	body := domain.CardBody{
		Text:    "OpenAI の API キーを登録します。",
		Options: []string{"やめる"},
		Secret:  &domain.SecretField{Provider: "openai", Label: "OpenAI", MaxBytes: 2560},
	}
	return domain.ThreadMessage{ChatMessage: domain.ChatMessage{
		ID: "c1", ProjectID: "p1", Kind: domain.KindQuestion, RefType: domain.RefOperation, RefID: "set_credential", Content: domain.EncodeCardBody(body),
	}, Open: open}
}

func render(t *testing.T, m domain.ThreadMessage) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, molecules.QuestionCard(m).Render(context.Background(), &sb))
	return sb.String()
}

// The card of an API key (#1140) has a masked, never refilled field that posts to the card's own endpoint, not to the messages of the thread.
func TestQuestionCard_AKeyCardHasAMaskedFieldThatPostsToItsOwnEndpoint(t *testing.T) {
	html := render(t, card(true))

	assert.Contains(t, html, `type="password"`)
	assert.Contains(t, html, `autocomplete="off"`)
	assert.Contains(t, html, `name="secret"`)
	assert.Contains(t, html, `hx-post="/projects/p1/messages/c1/secret"`)
	assert.Contains(t, html, `maxlength="2560"`)
	assert.NotRegexp(t, `<input[^>]*value=`, html, "the field is never filled in")
	assert.Contains(t, html, "やめる", "declining stays a button of the card")
	assert.Contains(t, html, "チャットの履歴には残りません")
}

func TestQuestionCard_AnAnsweredKeyCardHasNoField(t *testing.T) {
	html := render(t, card(false))

	assert.NotContains(t, html, `type="password"`)
	assert.Contains(t, html, "回答済み")
}

// Without scripts the form posts to the card's own endpoint (not the current page), and the field is described by the line that says where the value goes.
func TestQuestionCard_TheFormHasItsOwnActionAndTheFieldIsDescribed(t *testing.T) {
	html := render(t, card(true))

	assert.Contains(t, html, `action="/projects/p1/messages/c1/secret"`)
	assert.Contains(t, html, `aria-describedby="secret-help-c1"`)
	assert.Contains(t, html, `id="secret-help-c1"`)
}
