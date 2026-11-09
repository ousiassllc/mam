package declined_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing/declined"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A safety refusal that is taken back or negated leaves no "制約: …" line: it would be the opposite of what the user said (#1125).
func TestAbsorb_ASafetyRefusalThatIsTakenBackIsNoConstraint(t *testing.T) {
	for _, tail := range []string{
		"暴力は出さないわけではない", "暴力は出さないのは違う", "暴力は出さないでもいい", "暴力は出さない わけではない", "暴力は出さない　わけではない",
		"未成年は出さないわけではない", "暴力は出さないとは言っていない", "暴力は出さないのではない", "暴力は出さなくてもいい",
	} {
		t.Run(tail, func(t *testing.T) {
			text := "猫の動画を作りたい。" + tail
			assert.Empty(t, declined.Refusals(text))
			var b domain.Brief
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
			assert.Empty(t, b.AdditionalInstruction)
		})
	}
}

// A refusal that is taken back does not hide the others the text states, and one that goes on as a plain statement stays.
func TestRefusals_OnlyTheRefusalThatIsTakenBackGoes(t *testing.T) {
	assert.Equal(t, []string{"酒は出しません"}, declined.Refusals("猫の動画を作りたい。暴力は出さないわけではない。酒は出しません"))
	for _, tail := range []string{"暴力は出さない", "暴力は出さないよ", "暴力は出さない予定", "暴力は出さないが酒は出す"} {
		assert.Equal(t, []string{"暴力は出さない"}, declined.Refusals("猫の動画を作りたい。"+tail), tail)
	}
}
