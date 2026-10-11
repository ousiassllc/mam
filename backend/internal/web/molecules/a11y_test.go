package molecules_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

func TestAttachmentStrip_NamesEveryAttachmentDifferently(t *testing.T) {
	html := render(t, molecules.AttachmentStrip("p1", []domain.Asset{
		{ID: "a1", Type: domain.AssetImage}, {ID: "a2", Type: domain.AssetImage}, {ID: "a3", Type: domain.AssetVideo},
	}))

	assert.Contains(t, html, `alt="添付画像 1 / 3"`)
	assert.Contains(t, html, `alt="添付画像 2 / 3"`)
	assert.Contains(t, html, `aria-label="添付動画 3 / 3"`)
	assert.NotContains(t, html, `alt="添付画像"`)
}

func TestApprovalCard_CommentInputHasAnAccessibleName(t *testing.T) {
	html := render(t, molecules.ApprovalCard(domain.ThreadMessage{
		ChatMessage: domain.ChatMessage{ID: "m1", ProjectID: "p1", Kind: domain.KindApproval, RefID: "r1", ApprovalStatus: domain.ApprovalPending},
	}))

	input := regexp.MustCompile(`<input[^>]*name="comment"[^>]*>`).FindString(html)
	require.NotEmpty(t, input, html)
	assert.Contains(t, input, `aria-label="コメント"`)
}

func TestApprovalCard_EnterInTheCommentFieldDoesNotApprove(t *testing.T) {
	html := render(t, molecules.ApprovalCard(domain.ThreadMessage{
		ChatMessage: domain.ChatMessage{ID: "m1", ProjectID: "p1", Kind: domain.KindApproval, RefID: "r1", ApprovalStatus: domain.ApprovalPending},
	}))

	// Implicit submission (Enter) presses the form's first submit button; it must be an inert one.
	first := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(html)
	require.NotEmpty(t, first, html)
	assert.Contains(t, first, "disabled")
	assert.NotContains(t, first, `name="decision"`)
	assert.Less(t, strings.Index(html, first), strings.Index(html, `value="approve"`))
}

// The answer buttons of a card post the answer and the card they reply to in hx-vals.
func TestQuestionCard_AnswerButtonsSendTheAnswerAndTheReplyTargetInHxVals(t *testing.T) {
	html := render(t, molecules.QuestionCard(domain.ThreadMessage{
		ChatMessage: domain.ChatMessage{ID: "m1", ProjectID: "p", Kind: domain.KindQuestion, Content: domain.EncodeCardBody(domain.CardBody{Text: "どちら？", Options: []string{"A"}})},
		Open:        true,
	}))
	assert.Contains(t, html, `hx-vals="{&#34;content&#34;:&#34;A&#34;,&#34;reply_to_message_id&#34;:&#34;m1&#34;}"`)
}
