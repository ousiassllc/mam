package briefing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A negation of something else in the sentence never removes a stated constraint: only the keyword's own predicate counts (#1114).
func TestExtract_AnUnrelatedNegationKeepsTheConstraint(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"架空の成人女性が主人公で商品は紹介しない", "制約: 成人・架空"},
		{"架空の成人女性が主人公 商品は出てこない", "制約: 成人・架空"},
		{"架空の成人女性が主人公で他に誰もいない", "制約: 成人・架空"},
		{"成人の女性が主人公で、ナレーションは入れない", "制約: 成人"},
		{"服は着ている設定で、BGMはなし", "制約: 着衣"},
		{"成人女性が商品を持たない", "制約: 成人"},
		{"成人で、他の人物は出さない", "制約: 成人"},
		// a restriction to the keyword: the negation denies the others, so the keyword is stated
		{"成人しか出ない", "制約: 成人"},
		{"成人以外は出さない", "制約: 成人"},
		{"成人でない人物は出さない", "制約: 成人"},
		{"成人ではない人は登場させない", "制約: 成人"},
		{"20歳以上のみ登場する。未成年は出ない", "制約: 成人"},
		// the keyword's own predicate (or the keyword itself) is negated
		{"成人ではない", ""},
		{"成人でもない設定", ""},
		{"架空の人物で、成人ではない", "制約: 架空"},
		{"着衣なし", ""},
		{"成人じゃなくて未成年", ""},
		{"成人ではない人物が主人公", ""},
		{"成人ではないので出さない", ""},
		{"架空ではなく実在", ""},
		{"服は着ていない", ""},
		{"ノンフィクション", ""},
	} {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, briefing.RuleExtractor{}.Extract(tt.text).AdditionalInstruction)
		})
	}
}

// An inferred constraint is taken back by the user's explicit retraction of it; the lines the user wrote are never touched (#1114).
func TestAbsorb_ARetractedConstraintLeavesTheInferredLine(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"やっぱり成人じゃない", "テーマ: 朝のルーティン\n制約: 架空・着衣"},
		{"やっぱり架空ではない。着衣なしで", "テーマ: 朝のルーティン\n制約: 成人"},
		{"成人ではなく架空の女性", "テーマ: 朝のルーティン\n制約: 架空・着衣"},
		{"商品は紹介しない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣"},
		// a restriction to the keyword is no retraction of it
		{"成人しか出ない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣"},
		{"成人以外は出さない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣"},
		{"成人でない人物は出さない", "テーマ: 朝のルーティン\n制約: 成人・架空・着衣"},
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
