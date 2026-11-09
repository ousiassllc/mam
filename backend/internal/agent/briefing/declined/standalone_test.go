package declined_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing/declined"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A safety refusal that is all the text says ("未成年は出さない") is still a stated constraint: it is kept as a "制約: …" line, as it is next to a request (#1124).
func TestAbsorb_AStandaloneSafetyRefusalIsKeptAsAConstraint(t *testing.T) {
	for _, tt := range []struct{ text, instruction string }{
		{"未成年は出さない", "制約: 未成年は出さない"},
		{"暴力は出さない", "制約: 暴力は出さない"},
		{"暴力は出さない方向で", "制約: 暴力は出さない"},
		{"酒は出しません", "制約: 酒は出しません"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, []string{tt.instruction[len(briefing.ConstraintPrefix):]}, declined.Refusals(tt.text))
			var b domain.Brief
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(tt.text), tt.text)
			assert.Equal(t, tt.instruction, b.AdditionalInstruction)
			assert.Empty(t, b.Subject)
		})
	}
}

// The answer to the Subject question that is only a safety refusal asks the Subject again, and the constraint stays: the Subject answered next is read without it (#1124).
func TestApplyAnswer_AStandaloneSafetyRefusalAsTheSubjectAnswerKeepsTheConstraintWhenAskedAgain(t *testing.T) {
	for _, text := range []string{"未成年は出さない", "暴力は出さない"} {
		t.Run(text, func(t *testing.T) {
			b := askedAgain(t, text)
			assert.Equal(t, briefing.ConstraintPrefix+text, b.AdditionalInstruction)

			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, "猫"))
			assert.Equal(t, "猫", b.Subject)
			assert.Equal(t, briefing.ConstraintPrefix+text, b.AdditionalInstruction)
		})
	}
}
