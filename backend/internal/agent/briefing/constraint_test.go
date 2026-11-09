package briefing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// An inferred constraint is taken back by the user's explicit retraction of it; the lines the user wrote are never touched (#1114).
func TestAbsorb_ARetractedConstraintLeavesTheInferredLine(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"やっぱり成人じゃない", "テーマ: 朝のルーティン\n制約: 架空・着衣"},
		{"やっぱり架空ではない。着衣なしで", "テーマ: 朝のルーティン\n制約: 成人"},
		{"成人ではなく架空の女性", "テーマ: 朝のルーティン\n制約: 架空・着衣"},
		{"商品は紹介しない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣"},
		// a restriction to the keyword is no retraction of it (the refusal of the safety word 成人 is kept as stated, as it is next to a request: #1124)
		{"成人しか出ない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣"},
		{"成人以外は出さない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣・成人以外は出さない"},
		{"成人でない人物は出さない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣・成人でない人物は出さない"},
		// a negation after a particle or a short noun run is a retraction
		{"成人女性ではなく少女にして", "テーマ: 朝のルーティン\n制約: 架空・着衣"},
		{"成人は出さない", "テーマ: 朝のルーティン\n制約: 架空・着衣・成人は出さない"},
		{"成人にはしない", "テーマ: 朝のルーティン\n制約: 架空・着衣"},
		{"架空の人物ではなく実在の人物", "テーマ: 朝のルーティン\n制約: 成人・着衣"},
		{"着衣は着ない", "テーマ: 朝のルーティン\n制約: 成人・架空"},
		{"成人だけではない", "テーマ: 朝のルーティン\n制約: 架空・着衣"},
		// a restriction that carries the negation in a noun phrase is no retraction either
		{"服を着ていない人物は出さない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣"},
		{"成人ではない子どもは一切出さない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣"},
		{"服を着ていない人物は出す", "テーマ: 朝のルーティン\n制約: 成人・架空"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			b := intake("テーマは朝のルーティン。成人で、架空の女性。服は着ている設定")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(tt.text), tt.text)
			assert.Equal(t, tt.want, b.AdditionalInstruction)
			assert.Equal(t, domain.BriefSourceInferred, b.SourceOf(domain.BriefAdditionalInstruction))
		})
	}

	b := intake("成人の女性")
	text := "やっぱり成人じゃない"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Empty(t, b.AdditionalInstruction, "the line goes when no label is left")
}

func TestAbsorb_ARetractionNeverTouchesALineTheUserWrote(t *testing.T) {
	b := domain.Brief{AdditionalInstruction: "制約: 成人・架空\n成人の女性だけが出る"}
	text := "やっぱり成人じゃない"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "制約: 成人・架空\n成人の女性だけが出る", b.AdditionalInstruction)
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefAdditionalInstruction))
}
