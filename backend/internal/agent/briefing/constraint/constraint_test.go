package constraint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/constraint"
)

// A negation of something else in the sentence never removes a stated constraint: only the keyword's own predicate counts (#1114).
func TestStated_OnlyTheKeywordsOwnPredicateNegatesIt(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"架空の成人女性が主人公で商品は紹介しない", "成人・架空"},
		{"架空の成人女性が主人公 商品は出てこない", "成人・架空"},
		{"架空の成人女性が主人公で他に誰もいない", "成人・架空"},
		{"成人の女性が主人公で、ナレーションは入れない", "成人"},
		{"服は着ている設定で、BGMはなし", "着衣"},
		{"成人女性が商品を持たない", "成人"},
		{"成人で、他の人物は出さない", "成人"},
		// a restriction to the keyword: the negation denies the others, so the keyword is stated
		{"成人しか出ない", "成人"},
		{"成人以外は出さない", "成人"},
		{"成人でない人物は出さない", "成人"},
		{"成人ではない人は登場させない", "成人"},
		{"20歳以上のみ登場する。未成年は出ない", "成人"},
		// the keyword's own predicate (or the keyword itself) is negated
		{"成人ではない", ""},
		{"成人でもない設定", ""},
		{"架空の人物で、成人ではない", "架空"},
		{"着衣なし", ""},
		{"成人じゃなくて未成年", ""},
		{"成人ではない人物が主人公", ""},
		{"成人ではないので出さない", ""},
		{"架空ではなく実在", ""},
		{"服は着ていない", ""},
		{"ノンフィクション", ""},
		// the negation follows a particle or a short noun run ("成人は出さない" is the opposite of "成人しか出ない")
		{"成人は出さない", ""},
		{"成人はいない", ""},
		{"成人にはしない", ""},
		{"成人にしない", ""},
		{"成人は使わない", ""},
		{"成人の女性ではない", ""},
		{"成人女性ではなく少女にして", ""},
		{"成人だけではない", ""},
		{"成人のみじゃない", ""},
		{"架空の人物ではない", ""},
		{"架空の人物ではなく実在の人物", ""},
		{"架空にはしない", ""},
		{"着衣は着ない", ""},
		{"着衣しない", ""},
		// the negation belongs to something else: the keyword stays
		{"成人女性が商品を持たない", "成人"},
		{"成人の女性が主人公で、ナレーションは入れない", "成人"},
		{"成人の女性でない人物は出さない", "成人"},
		{"成人のみ。服を着ない", "成人"},
		{"成人だけ出す", "成人"},
		{"成人以外を出す", "成人"},
		// a negation that modifies the noun after it, which the sentence then leaves out: the restriction to the keyword is stated
		{"服を着ていない人物は出さない", "着衣"},
		{"服を着ない人物は出さない", "着衣"},
		{"服を着ていない人は登場させない", "着衣"},
		{"服を着ていないキャラクターは使わない", "着衣"},
		{"着衣しない人物は出さない", "着衣"},
		{"成人ではない人物は一切出さない", "成人"},
		{"成人ではない人物は画面に出さない", "成人"},
		{"成人ではない子どもは出さない", "成人"},
		{"成人でない子供は絶対に出さない", "成人"},
		{"成人ではないキャラは決して登場させない", "成人"},
		// any short word or a comma in front of the refusing verb keeps the restriction
		{"成人ではない人物はもちろん出さない", "成人"},
		{"成人ではない人物は断じて出さない", "成人"},
		{"成人ではない人物はぜひ出さないで", "成人"},
		{"成人ではない人物、出さない", "成人"},
		{"成人ではない人物は、出さない", "成人"},
		{"服を着ていない人物はもちろん出さない", "着衣"},
		// a verb of another clause ends the run: the noun is not left out
		{"成人ではない人物は出すが他は出さない", ""},
		{"成人ではない。人物は出す", ""},
		// a full stop ends the negation of the keyword: the next sentence is another one
		{"成人ではない。人物は出さない", ""},
		{"主人公は成人ではない。人物は出さない", ""},
		{"主人公は動物で成人ではない。人間は出さない", ""},
		// the verb must end its clause: a word that takes the refusal back leaves the keyword denied
		{"成人ではない人物は出さないわけではない", ""},
		{"成人ではない人物は出さないとは言っていない", ""},
		{"成人ではない人物は出さないことはない", ""},
		{"成人ではない人物は出さないとは限らない", ""},
		{"成人ではない人物は出さない。", "成人"},
		{"成人ではない人物は出さないので", "成人"},
		{"成人ではない。人物が主人公", ""},
		// the same negation, but the sentence does not leave the noun out: the keyword is denied
		{"服を着ていない人物は出す", ""},
		{"服を着ていない人物が主人公", ""},
		{"服を着ない人物を主役に", ""},
		{"服を着ていない人物は出てもいい", ""},
		{"成人ではない人物は出す", ""},
		{"成人ではない子どもは出してもいい", ""},
		{"服を着ていない", ""},
	} {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, strings.Join(constraint.Stated(tt.text), "・"))
		})
	}
}
