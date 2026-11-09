package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// B is an ordinary noun that is neither a person nor a product the list knows (#1129, the known limit of the design): "猫ではなく{B}の動画にして" is a contradiction to ask about,
// never a change, and never lost: the decided Subject stays, B is the proposal, one card. The list is not widened to every noun on purpose (a season, a time, a color: "冬ではなく夏の動画"
// must never replace); the price is one confirmation card.
func TestAbsorbAndApplyAnswer_AnOrdinaryNounThatIsNoPersonOrProductIsOnlyProposed(t *testing.T) {
	for _, b := range []string{"パンダ", "車", "コーヒー", "スニーカー", "社長", "化粧水", "店", "OL", "シニア", "うさぎ"} {
		for _, noun := range []string{"動画", "CM"} {
			text := "猫ではなく" + b + "の" + noun + "にして"
			t.Run(text, func(t *testing.T) {
				f := decided("猫", "")
				briefing.Absorb(&f, briefing.RuleExtractor{}.Extract(text), text)
				assert.Equal(t, "猫", f.Subject, "the decided Subject is not replaced")
				assert.Equal(t, b, f.ProposedSubject, "B is the one thing to ask about")
				a := decided("猫", "")
				assert.NotNil(t, briefing.ApplyAnswer(&a, domain.BriefSubject, text), "asked once")
				assert.Equal(t, "猫", a.Subject)
				assert.Equal(t, b, a.ProposedSubject)
			})
		}
	}
}
