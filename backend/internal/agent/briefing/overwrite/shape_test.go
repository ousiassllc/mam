package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing/overwrite"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A later sentence that takes the change back, or any sentence after the statement, keeps the Subject; so does a question (review: "…に変更して。やっぱりやめます").
var takenBackChanges = []string{
	"主人公を犬に変更して。やっぱりやめます", "主人公を犬に変更して。いや、やっぱり猫のまま", "主人公を犬に変更して。いや待って", "主人公は犬にします。でも迷っています",
	"主人公を犬に変更して。後で相談", "主人公を犬に変更して。ただし後で決める", "主人公を犬に変更してください。あ、でも猫のままで", "主人公を犬に変更して。取り消し",
	"主人公を犬に変更して\nやっぱりやめる", "主人公を犬に変更して？", "主人公を犬に変更して。30秒にして。やっぱり迷う",
}

func TestApplyAnswerAndAbsorb_ATakenBackOrQuestionedChangeKeepsTheSubject(t *testing.T) {
	for _, text := range takenBackChanges {
		for _, proposed := range []string{"", "ロボット"} {
			t.Run("answer "+text+"/"+proposed, func(t *testing.T) {
				b := decided("猫", proposed)
				briefing.ApplyAnswer(&b, domain.BriefSubject, text)
				assert.Equal(t, "猫", b.Subject)
			})
		}
		t.Run("absorb "+text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
		})
	}
}

// A sentence about another item in front of the change does not matter: the change is the last word.
func TestApplyAnswer_AnEarlierSentenceAboutAnotherItemDoesNotHideTheChange(t *testing.T) {
	b := decided("猫", "")
	text := "30秒にして。主人公を犬に変更して"
	assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
	assert.Equal(t, "犬", b.Subject)
}

// A fragment that is no noun phrase is neither the Subject nor a one-click proposal, on the free-text path too.
func TestAbsorb_AFragmentIsNotProposedAsTheSubject(t *testing.T) {
	for _, text := range []string{"主人公はそのままで", "主人公は犬にしたいけど迷う", "主人公は犬かな", "主人公は犬w", "主人公は犬のはず", "主人公は犬か猫", "題材は猫のままで"} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
			assert.Empty(t, b.ProposedSubject)
		})
	}
	b := decided("猫", "")
	text := "ロボットを主人公にした動画も作りたい"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "ロボット", b.ProposedSubject, "a noun phrase is still a contradiction to ask about")
}

func TestPlausible(t *testing.T) {
	for v, want := range map[string]bool{
		"猫": true, "ロボット": true, "架空の30代女性インフルエンサー": true, "かわいい猫": true, "ミュージカル": true,
		"迷う": false, "そのまま": false, "犬かな": false, "犬w": false, "犬のはず": false, "犬か猫": false, "変えなくていい": false, "": false,
	} {
		assert.Equal(t, want, overwrite.Plausible(v), v)
	}
}

// In the "A ではなく B" form the value is the noun phrase only: a tail that is a verb, a condition, a hedge, a request or a second clause is no Subject
// and no evidence (review: "猫ではなく犬にして、30秒にして" gave "犬にして、30秒"). Same for a hedge or a third party in front of the denied value.
func TestApplyAnswerAndAbsorb_AContrastThatIsNotAPlainStatementKeepsTheSubject(t *testing.T) {
	for _, text := range []string{
		"猫ではなく犬にして、30秒にして", "猫ではなく犬にするなら", "猫ではなく犬にしてません", "猫ではなく犬にしても", "猫ではなく犬を希望", "猫ではなく犬を検討中",
		"猫ではなく犬でしたっけ", "猫ではなく犬にしたかったけど", "猫ではなく犬にしたら", "猫ではなく犬にした場合", "猫ではなく犬にしますが", "猫ではなく犬にしますけど",
		"猫ではなく犬にして30秒", "猫ではなく犬にして、猫も出して", "猫ではなく犬でも可",
		"多分猫ではなく犬にします", "たぶん猫ではなく犬に変更して", "もし猫ではなく犬にします", "できれば猫ではなく犬にします", "嘘だけど猫ではなく犬にします",
		"迷うけど猫ではなく犬にします", "友達は猫ではなく犬にします", "上司は猫ではなく犬に変更して", "AIは猫ではなく犬にします", "昨日は猫ではなく犬にします",
	} {
		for _, proposed := range []string{"", "ロボット"} {
			t.Run("answer "+text+"/"+proposed, func(t *testing.T) {
				b := decided("猫", proposed)
				briefing.ApplyAnswer(&b, domain.BriefSubject, text)
				assert.Equal(t, "猫", b.Subject)
			})
		}
		t.Run("absorb "+text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
		})
	}
}

// A value that is a list, an alternative, a pointer or a placeholder is no Subject: a plain "…に変更して" with it keeps the decided one.
func TestApplyAnswerAndAbsorb_APlaceholderOrAListIsNoSubject(t *testing.T) {
	for _, v := range []string{"犬or猫", "犬または猫", "犬あるいは猫", "犬もしくは猫", "犬/猫", "犬・猫", "犬とロボット", "犬や猫", "これ", "それ", "前者", "両方", "全部", "適当", "ランダム", "デフォルト", "TBD", "null", "XX", "-", "未設定", "ダミー"} {
		text := "主人公を" + v + "に変更して"
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.ApplyAnswer(&b, domain.BriefSubject, text)
			assert.Equal(t, "猫", b.Subject)
			b = decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
		})
	}
}

// Nouns of every script still replace it by an explicit change.
func TestApplyAnswer_NounsOfEveryScriptStillReplaceByAnExplicitChange(t *testing.T) {
	for v, ok := range map[string]bool{"犬": true, "ロボット": true, "ねこ": true, "かわいい犬": true, "小さな犬": true, "架空の30代女性インフルエンサー": true, "AIキャラクター": true, "iPhone": true, "うさぎ": true, "ミュージカル": true} {
		text := "主人公を" + v + "に変更して"
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, ok, overwrite.Plausible(v))
			b := decided("猫", "")
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, v, b.Subject)
		})
	}
}

// The statement is judged where the value FIRST stands: a hedged, cancelled or third-party change in an earlier sentence is not hidden by a last
// sentence that only repeats the value ("…変更しようか迷う。犬"), and a hedge or a deferral fused to the value is no Subject ("多分犬", "保留中", "別の動物").
func TestApplyAnswerAndAbsorb_ARepeatedValueOrAFusedHedgeIsNoEvidence(t *testing.T) {
	for _, text := range []string{
		"主人公を犬に変更しようか迷う。犬", "主人公を犬に変更するのはやめます。犬", "たぶん主人公を犬に変更して。犬", "上司は主人公を犬に変更してと言った。犬",
		"主人公を犬に変更して。やっぱりやめます。犬", "もし主人公を犬に変更するなら。主人公は犬", "友達は主人公を犬に変更して。犬",
		"主人公を多分犬にして", "主人公を明日犬にして", "主人公を来週犬にして", "主人公を次回犬にして", "主人公を一旦犬にして", "主人公を一応犬にして", "主人公をワンチャン犬にして",
		"猫ではなく、暫定犬にして", "主人公をmaybe犬にして", "主人公を保留中にして", "主人公を確認中にして", "主人公を調整中にして", "主人公を相談次第にして", "主人公を要検討にして",
		"主人公を未確定にして", "主人公を後日にして", "主人公をpendingにして", "主人公をTBCにして", "主人公をdummyにして", "主人公を上記にして", "主人公を以下にして",
		"主人公をみんなにして", "主人公を3にして", "猫ではなく別の動物にして", "猫ではなく他の動物にして", "猫ではなく例の動物にして", "猫ではなく前回の動物にして",
	} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.ApplyAnswer(&b, domain.BriefSubject, text)
			assert.Equal(t, "猫", b.Subject)
			b = decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
		})
	}
}

// A plain explicit change in chat is applied whatever finished verb it ends in (review: "主人公を猫に変えて" regressed to a question card).
func TestAbsorb_PlainChangeVerbsReplaceTheSubject(t *testing.T) {
	for text, want := range map[string]string{
		"主人公を犬に変えて": "犬", "主人公を犬に変えたい": "犬", "主人公を犬に差し替えて": "犬",
		"題材を犬に変えて": "犬", "主人公を猫から犬に変更して": "犬", "主人公をやっぱり犬に変更して": "犬", "主人公を犬に変更でお願いします": "犬", "猫でなく犬にして": "犬",
		"主人公を犬に変更してください": "犬", "主人公を犬にして": "犬",
	} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, want, b.Subject)
		})
	}
}

// A value that is no fragment but not a noun by shape (a modifier verb, a hiragana noun) is never replaced at once but is proposed: the user taps it. A chat
// change with it is not dropped (review: "主人公を空飛ぶ猫に変更して" got no card at all).
func TestAbsorb_ANounOutsideTheShapeIsProposedNotDropped(t *testing.T) {
	for _, v := range []string{"空飛ぶ猫", "お母さん", "ぞう", "ふわふわの猫", "喋る犬", "猫と犬", "お姫様"} {
		text := "主人公を" + v + "に変更して"
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject, "not replaced at once")
			assert.Equal(t, v, b.ProposedSubject, "proposed, so the card is asked")
		})
	}
}

// The buttons: the exact text of a button picks that value, whatever shape the other value has ("猫の動画" is the Subject, not "猫" + "の動画").
func TestApplyAnswer_AButtonAlwaysPicksItsOwnValue(t *testing.T) {
	for _, c := range []struct{ current, proposed string }{
		{"猫の動画", "猫"}, {"猫動画", "猫"}, {"猫のCM", "猫"}, {"猫の話", "猫"}, {"動画の猫", "猫"}, {"猫", "猫の動画"},
	} {
		t.Run(c.current+"/"+c.proposed, func(t *testing.T) {
			b := decided(c.current, c.proposed)
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, c.current))
			assert.Equal(t, c.current, b.Subject)
			assert.Empty(t, b.ProposedSubject)
			b = decided(c.current, c.proposed)
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, c.proposed))
			assert.Equal(t, c.proposed, b.Subject)
		})
	}
}

// A plain noun that merely CONTAINS a piece of a fragment word ("泣いている赤ちゃん", "たらこ", "Twitter", "ミケ（猫）") is no fragment: the change is not dropped silently
// (review: the user got "読み取れる内容がありませんでした" and no card). Replaced or proposed, never lost.
func TestAbsorb_ANounThatContainsAFragmentPieceIsNotDropped(t *testing.T) {
	for _, v := range []string{"泣いている赤ちゃん", "歌っている猫", "笑っている女の子", "ミケ（猫）", "Twitter", "Tiffany", "Newjeans", "たらこ", "とかげ", "はずかしがり屋の犬"} {
		text := "主人公を" + v + "に変更して"
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.True(t, b.Subject == v || b.ProposedSubject == v, "replaced or proposed, not lost: S=%q P=%q", b.Subject, b.ProposedSubject)
		})
	}
}

// A list made with a symbol is no candidate either.
func TestAbsorb_ASymbolListIsNotProposed(t *testing.T) {
	for _, v := range []string{"犬・猫", "犬/猫", "犬／猫", "犬&猫", "犬+猫"} {
		text := "主人公を" + v + "に変更して"
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
			assert.Empty(t, b.ProposedSubject)
		})
	}
}
