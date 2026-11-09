package briefing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The theme and constraint lines the extractor wrote are no topic, whether or not the user has saved or confirmed them since (Adopt makes
// them the user's); a line the user wrote may be one (#1108, #1114).
func TestMissing_ExtractorLinesNameNoSubject(t *testing.T) {
	b := domain.Brief{AdditionalInstruction: "制約: 成人", Sources: map[string]string{string(domain.BriefAdditionalInstruction): domain.BriefSourceInferred}}
	assert.Contains(t, b.Missing(), domain.BriefSubject)
	b.Sources = nil
	assert.Contains(t, b.Missing(), domain.BriefSubject, "saved or confirmed: still only the extractor's line")
	b.AdditionalInstruction = "テーマ: 朝の一枚\n制約: 成人・架空"
	assert.Contains(t, b.Missing(), domain.BriefSubject)
	b.AdditionalInstruction = "制約: 成人\n猫の日常を撮る"
	assert.NotContains(t, b.Missing(), domain.BriefSubject, "a line the user wrote names the topic")
	b.Duration, b.Sources = "15秒", map[string]string{string(domain.BriefDuration): domain.BriefSourceRecommended}
	b.Set(domain.BriefDuration, "30秒")
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefDuration), "a value the user sets is the user's")
}

func TestApplyEdit_GivenSetsAndTheContradictionIsClearedOnlyBySubjectOrConfirm(t *testing.T) {
	inferred := func() domain.Brief {
		b := intake(firstRequest)
		b.ProposedSubject = "ロボット"
		return b
	}
	values := func(b domain.Brief) map[domain.BriefField]string {
		return map[domain.BriefField]string{domain.BriefSubject: b.Subject, domain.BriefMood: b.Mood}
	}

	b := inferred()
	briefing.ApplyEdit(&b, values(b), b.AdditionalInstruction, []domain.BriefField{domain.BriefMood}, false)
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefMood), "given, though equal")
	assert.Equal(t, domain.BriefSourceInferred, b.SourceOf(domain.BriefSubject))
	assert.Equal(t, "ロボット", b.ProposedSubject, "the Subject was not given: the contradiction stays")

	b = inferred()
	briefing.ApplyEdit(&b, values(b), b.AdditionalInstruction, []domain.BriefField{domain.BriefSubject}, false)
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefSubject))
	assert.Empty(t, b.ProposedSubject, "the Subject was given")

	b = inferred()
	briefing.ApplyEdit(&b, values(b), b.AdditionalInstruction, nil, true)
	assert.Empty(t, b.ProposedSubject, "confirmed")
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefSubject))
}
