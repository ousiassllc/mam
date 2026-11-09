package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A comma clause after "A ではなく B にして" that states a Target, a Mood or a platform is another item of the request, whatever word it is made of: B is read on its own on a free message,
// on a card answer and on an empty Brief, and the decided Subject is never replaced (#1133: B was lost, or the whole clause became the Subject).
func TestAbsorbAndApplyAnswer_AClauseOfAnyTargetOrMoodWordAfterAContrastNeverLosesB(t *testing.T) {
	for _, tail := range []string{"社会人で", "社会人にして", "シニアにして", "女子高生にして", "おじいさんにして", "おしゃれにして", "カジュアルにして", "実写にして", "インスタで", "ロボットにして"} {
		text := "猫ではなく犬にして、" + tail
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
			assert.Equal(t, "犬", b.ProposedSubject)
			a := decided("猫", "")
			assert.NotNil(t, briefing.ApplyAnswer(&a, domain.BriefSubject, text), "asked once")
			assert.Equal(t, "猫", a.Subject)
			assert.Equal(t, "犬", a.ProposedSubject)
			var e domain.Brief
			briefing.Absorb(&e, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "犬", e.Subject, "the clause is no part of the Subject")
		})
	}
}
