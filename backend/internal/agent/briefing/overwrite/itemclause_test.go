package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The clause that is stripped IS the item value: a take-back, a negation, a hedge or a noun glued to it keeps the clause, so the Subject is kept (review of 2cafe5f0).
func TestAbsorbAndApplyAnswer_ATakeBackOrANounGluedToTheItemIsNotStripped(t *testing.T) {
	for _, tail := range []string{
		"30秒却下", "30秒撤回", "縦型禁止", "30秒キャンセル", "30秒×", "30秒保留", "30秒NG", "30秒未確定", "30秒or60秒", "30秒TBD",
		"30秒猫", "縦型猫に", "猫30秒で", "30秒or猫", "30秒ロボット", "30秒ロボットで", "却下", "無し", "ロボット",
	} {
		text := "主人公を犬に変更して、" + tail
		for _, proposed := range []string{"", "ロボット"} {
			t.Run(text+"/"+proposed, func(t *testing.T) {
				b := decided("猫", proposed)
				briefing.ApplyAnswer(&b, domain.BriefSubject, text)
				assert.NotEqual(t, "犬", b.Subject)
				b = decided("猫", proposed)
				briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
				assert.NotEqual(t, "犬", b.Subject)
			})
		}
	}
}

// The plain item clauses are still no second sentence, one or several.
func TestAbsorbAndApplyAnswer_PlainItemClausesAreStillStripped(t *testing.T) {
	for _, tail := range []string{"30秒で", "縦型で", "30秒にして", "縦型にして", "30秒がいい", "30秒でお願いします", "30秒で、縦型で", "16:9で", "1分で"} {
		text := "主人公を犬に変更して、" + tail
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.ApplyAnswer(&b, domain.BriefSubject, text)
			assert.Equal(t, "犬", b.Subject)
			b = decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "犬", b.Subject)
		})
	}
}

// A safety constraint stated next to the item clause is still recorded.
func TestApplyAnswer_ASafetyConstraintBesideAnItemClauseIsRecorded(t *testing.T) {
	for _, text := range []string{"主人公を犬に変更して、暴力は出さない", "主人公を犬に変更して、30秒で、暴力は出さない", "主人公を犬に変更して、暴力は出さない、30秒で"} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.ApplyAnswer(&b, domain.BriefSubject, text)
			assert.Contains(t, b.AdditionalInstruction, "暴力は出さない")
		})
	}
}
