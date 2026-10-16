package a11y_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// The buttons of a chat card that can wait in the thread next to another one of its kind name their
// card (#807): several World State proposals are pending at once, and so are several questions.

func pendingApproval(id, content string) domain.ThreadMessage {
	return domain.ThreadMessage{ChatMessage: domain.ChatMessage{
		ID: id, Kind: domain.KindApproval, Content: content, ApprovalStatus: domain.ApprovalPending,
		RefType: domain.RefNotePromotion,
	}}
}

func TestApprovalCard_ButtonsAndCommentAreNamedAfterTheirCard(t *testing.T) {
	html := render(t, molecules.ApprovalCard(pendingApproval("a1", "世界観 v2 を反映する"))) +
		render(t, molecules.ApprovalCard(pendingApproval("a2", "口調 v1 を反映する")))

	assert.Equal(t, []string{"承認: 世界観 v2 を反映する", "承認: 口調 v1 を反映する"}, buttonNames(html, "承認"))
	assert.Equal(t, []string{"修正を依頼: 世界観 v2 を反映する", "修正を依頼: 口調 v1 を反映する"}, buttonNames(html, "修正を依頼"))

	var comments []string
	for _, m := range regexp.MustCompile(`<input[^>]*aria-label="([^"]*)"`).FindAllStringSubmatch(html, -1) {
		comments = append(comments, m[1])
	}
	assert.Equal(t, []string{"コメント: 世界観 v2 を反映する", "コメント: 口調 v1 を反映する"}, comments)
}

func openCard(kind domain.MessageKind, id, text string) domain.ThreadMessage {
	return domain.ThreadMessage{Open: true, ChatMessage: domain.ChatMessage{
		ID: id, Kind: kind, Content: domain.EncodeCardBody(domain.CardBody{Text: text, Options: []string{"はい"}}),
	}}
}

func TestQuestionCard_OptionButtonsAreNamedAfterTheirCard(t *testing.T) {
	html := render(t, molecules.QuestionCard(openCard(domain.KindQuestion, "q1", "動画の目的は何ですか"))) +
		render(t, molecules.QuestionCard(openCard(domain.KindQuestion, "q2", "尺は何秒ですか")))

	assert.Equal(t, []string{"はい: 動画の目的は何ですか", "はい: 尺は何秒ですか"}, buttonNames(html, "はい"))
	assert.Equal(t, []string{"AIに任せる: 動画の目的は何ですか", "AIに任せる: 尺は何秒ですか"}, buttonNames(html, domain.DelegateOption))
}

func TestEscalationCard_OptionButtonsAreNamedAfterTheirCard(t *testing.T) {
	html := render(t, molecules.EscalationCard(openCard(domain.KindEscalation, "e1", "予算を超えます"))) +
		render(t, molecules.EscalationCard(openCard(domain.KindEscalation, "e2", "素材が足りません")))

	assert.Equal(t, []string{"はい: 予算を超えます", "はい: 素材が足りません"}, buttonNames(html, "はい"))
}
