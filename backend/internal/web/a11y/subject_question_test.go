package a11y_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// #1108: the Subject is not left to the AI, so its question has no "AIに任せる" button; the other Brief questions keep it.
func TestQuestionCard_SubjectHasNoDelegateButton(t *testing.T) {
	card := func(f domain.BriefField) string {
		m := openCard(domain.KindQuestion, "q1", f.Question())
		m.RefType, m.RefID = domain.RefBriefField, string(f)
		return render(t, molecules.QuestionCard(m))
	}

	assert.NotContains(t, card(domain.BriefSubject), domain.DelegateOption)
	assert.Contains(t, card(domain.BriefMood), domain.DelegateOption)
}
