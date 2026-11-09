package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// "A ではなく B の○○にして" replaces the decided Subject with every noun that says the video is about B ("の動画", "のCM", "の広告"): the reader of the contrast and the shape
// the evidence has (overwrite.Committed) name the same nouns, so none of them is only a card (#1127: "犬の広告にして" was proposed, "犬の動画にして" a change).
func TestAbsorbAndApplyAnswer_AContrastWithAnyNounOfTheVideoReplacesTheSubject(t *testing.T) {
	for _, noun := range []string{"動画", "CM", "広告"} {
		text := "猫ではなく犬の" + noun + "にして"
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "犬", b.Subject)
			assert.Empty(t, b.ProposedSubject)
			a := decided("猫", "")
			assert.Nil(t, briefing.ApplyAnswer(&a, domain.BriefSubject, text), "nothing to ask")
			assert.Equal(t, "犬", a.Subject)
			assert.Empty(t, a.ProposedSubject)
		})
	}
}
