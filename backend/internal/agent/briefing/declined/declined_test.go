package declined_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing/declined"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// laterDeferrals put the decision off with 後で / あとで. The rule fails closed: whatever follows the word, the clause is a deferral unless it is a verb that modifies the noun
// ending the phrase ("後で見返したくなる猫"), so a noun, a request, a nominalized form or any other ending is a deferral without being listed (#1114).
var laterDeferrals = []string{
	"後でまた決めます", "後で見直します", "後でやります", "後で確認します", "後で入力します", "後で送ります", "後で調整します", "後で書きます", "後で言います",
	"あとでまた考える", "後で猫にします", "後で猫", "あとで猫の動画", "やっぱり後で猫にします", "また後で猫の動画を作りたい", "後でかわいい猫にします",
	// a noun
	"題材は後で相談", "主人公は後で検討", "主人公は後で決定", "主人公は後で変更", "主人公は後で追加", "主人公は後で指定", "主人公は後で選択", "それは後で相談",
	// a request
	"題材は後でお願い", "主人公は後で決めて", "主人公は後で決めてね", "題材は後で考えさせて", "主人公は後で決めさせてほしい",
	// a nominalized form
	"主人公は後で決めるつもり", "主人公は後で決める予定", "主人公は後で決める方向で", "題材は後で連絡する予定", "主人公は後で決める方針", "主人公は後で決める形で", "主人公は後で決める前提",
	// leave it as it is
	"題材は後でいいや", "題材は後でいいよ", "題材は後でOK", "題材は後でも良い", "題材は後でいいです", "題材は後で大丈夫", "題材は後でどうでも",
}

// laterDeferralsWithAnotherSentence name a Subject in one sentence and put the Subject itself off in another: the answer is vague, so it is asked again.
var laterDeferralsWithAnotherSentence = []string{"猫の動画。題材は後で相談", "猫の動画。それは後で相談", "猫の動画を作りたい。主人公は後で決める"}

func TestApplyAnswer_ALaterDeferralIsNeverTheSubject(t *testing.T) {
	for _, text := range laterDeferrals {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			again := briefing.ApplyAnswer(&b, domain.BriefSubject, text)
			if assert.NotNil(t, again, "asked again") {
				assert.Equal(t, string(domain.BriefSubject), again.RefID)
			}
			assert.Empty(t, b.Subject)
		})
	}
}

func TestApplyAnswer_ADeferralOfTheSubjectInAnotherSentenceIsNeverTheSubject(t *testing.T) {
	for _, text := range laterDeferralsWithAnotherSentence {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			assert.NotNil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text), "asked again")
			assert.Empty(t, b.Subject)
		})
	}
}

// A deferral of another matter in a later sentence leaves the Subject the first sentence states (Extract judges per sentence, so does the answer).
func TestApplyAnswer_ADeferralOfAnotherMatterInALaterSentenceKeepsTheSubject(t *testing.T) {
	for text, want := range map[string]string{
		"猫の動画を作りたい。後で見返したい":      "猫の動画を作りたい",
		"猫の動画を作りたい。後で詳細を決める":     "猫の動画を作りたい",
		"猫の動画を作りたい。後で細かい点を相談する":  "猫の動画を作りたい",
		"猫の動画を作りたい。後で見返したい。後で送る": "猫の動画を作りたい",
	} {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, want, b.Subject)
			assert.Equal(t, "猫", briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

// A deferral in the first sentence, or a vague one, still vetoes the answer: only a later sentence about something else is set aside.
func TestApplyAnswer_ADeferralInTheFirstSentenceStillVetoes(t *testing.T) {
	for _, text := range []string{"後で決める。猫の動画を作りたい", "後で見返したい", "まだ決まっていない。猫の動画を作りたい。後で見返したい", "後で決める。後で見返したい"} {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			assert.NotNil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Empty(t, b.Subject)
		})
	}
}

func TestExtract_ALaterDeferralStatesNoSubject(t *testing.T) {
	for _, text := range laterDeferrals {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

// Only the noun-modifier reading of 後で is no deferral, and a 後 that follows a Han character is part of a word.
func TestApplyAnswer_ANounPhraseWithAfterwardsIsASubject(t *testing.T) {
	for text, want := range map[string]string{
		"後で見返したくなる猫":     "後で見返したくなる猫",
		"放課後で踊る猫":        "放課後で踊る猫",
		"食後でくつろぐ猫":       "食後でくつろぐ猫",
		"未定義の世界を旅するロボット": "未定義の世界を旅するロボット",
		"主人公は後で見返したくなる猫": "後で見返したくなる猫",
		// the verb ending is any of the plain endings, not only る・た・ない・たい
		"後で泣く猫":         "後で泣く猫",
		"後で歌う猫":         "後で歌う猫",
		"後で笑う猫の動画":      "後で笑う猫の動画",
		"後で泣く猫の動画を作りたい": "後で泣く猫の動画を作りたい",
		"後で踊る猫":         "後で踊る猫",
		"後で喋る猫":         "後で喋る猫",
		"後で飛ぶ鳥":         "後で飛ぶ鳥",
		"後で遊ぶ犬":         "後で遊ぶ犬",
		"後で読む本":         "後で読む本",
		"後で泣いた猫":        "後で泣いた猫",
		"後で寝ない猫":        "後で寝ない猫",
	} {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, want, b.Subject)
		})
	}
}

// A refusal in the same comma-less clause as the request leaves the Subject the request states: the constraint is read apart (constraint.Stated) (#1114).
func TestExtract_ARefusalRunningOnFromTheRequestKeepsTheSubject(t *testing.T) {
	for text, want := range map[string]string{
		"猫の動画を作りたいが暴力は出さない":       "猫",
		"猫の動画を作りたいけど暴力は出さない":      "猫",
		"ロボットの動画を作りたくて酒は出しません":    "ロボット",
		"ロボットの動画にして酒は出しません":       "ロボット",
		"猫の動画は作らないが犬の動画は作りたい":     "犬",
		"成人しか出さない猫の動画":            "成人しか出さない猫",
		"成人しか出さない猫の動画にして":         "成人しか出さない猫",
		"架空の女性を主人公にして未成年は出さない":    "架空の女性",
		"猫の動画を作りたい、暴力は出さない":       "猫",
		"猫の動画が作りたくて暴力は出しません":      "猫",
		"ロボットの動画を作りたいので暴力は出さない":   "ロボット",
		"ロボットの動画を作りたいけれど暴力は出しません": "ロボット",
		// the refusal ends in a nominalizer and then "で" with the request in the same comma-less clause: the part with the request keeps its Subject
		"暴力は出さない方向で猫の動画を作りたい":  "猫",
		"猫の動画を暴力は出さない形で作りたい":   "猫",
		"お酒は出さない前提で猫の動画を作りたい":  "猫",
		"猫の動画を作りたい。暴力は出さない方向で": "猫",
		"お酒は出さない予定で猫の動画を作って":   "猫",
		// the relative clause on a nominalizer noun is no part of the Subject
		"酒を出さない人の動画": "人",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

// A refusal that is the whole predicate of the part holding the noun states no Subject, whatever runs on after it (#1114).
func TestExtract_AWholeRefusalStatesNoSubject(t *testing.T) {
	for _, text := range []string{
		"猫の動画は作らない", "猫の動画は作りません", "猫の動画が作りたくない", "猫の動画は作らないが暴力は出さない", "猫の動画は出しません",
		"猫の動画は作らない方向で", "猫は出さない予定です", "猫は出さない方針", "猫は出さない事にした", "猫の動画は作らない人",
		"猫は出さないつもり", "猫は出さない予定なので", "猫は出さない方向でお願いします",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)

			var b domain.Brief
			again := briefing.ApplyAnswer(&b, domain.BriefSubject, text)
			assert.NotNil(t, again, "asked again")
			assert.Empty(t, b.Subject)
		})
	}
}

// A relative clause of an absence verb in front of the head noun is no part of the Subject (#1114).
// Every path that reads the Subject drops it: "…を主人公にした", "主人公は…" and "題材は…" give the head noun too.
func TestExtract_ARelativeClauseThatRefusesIsNotTheSubject(t *testing.T) {
	for text, want := range map[string]string{
		"暴力を出さない猫の動画を作りたい":   "猫",
		"暴力を出さない猫を主人公にした動画":  "猫",
		"お酒を出さない人物を主人公にした動画": "人物",
		"出さない猫を主人公にして":       "猫",
		"暴力を出さない猫が主人公":       "猫",
		"題材は出さない猫":           "猫",
		"主人公は出さない猫":          "猫",
		"暴力を出しません猫を主人公にして":   "猫",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

// A relative clause on a nominalizer noun (人, 形 …) is no refusal of the clause: the answer keeps its words.
func TestApplyAnswer_ARelativeClauseOnANominalizerNounIsNoRefusal(t *testing.T) {
	for _, text := range []string{"お酒を出さない人の話を作りたい", "暴力を出さない形の動画を作りたい", "酒を出さない人の動画"} {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.NotEmpty(t, b.Subject)
		})
	}
}

// A word that only looks like a deferral inside an ordinary word is no deferral (#1114).
func TestExtract_AWordInsideAnOrdinaryWordIsNoDeferral(t *testing.T) {
	for text, want := range map[string]string{
		"放課後でダンスする猫の動画":          "放課後でダンスする猫",
		"主人公は放課後で踊る猫":            "放課後で踊る猫",
		"食後でくつろぐ猫の動画":            "食後でくつろぐ猫",
		"未定義の世界を旅するロボットの動画":      "旅するロボット",
		"後で見返したくなる猫の動画を作りたい":     "後で見返したくなる猫",
		"主人公は後で見返したくなる猫":         "後で見返したくなる猫",
		"主人公は猫、あとで見返したい":         "猫",
		"主人公は猫、後で変更します":          "猫",
		"主人公を猫に変更して":             "猫",
		"未定義の世界を描くのはやめたい、猫の動画":   "猫",
		"未定義の世界を旅するロボットの動画を作りたい": "旅するロボット",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

func TestDeferred(t *testing.T) {
	for _, s := range slices.Concat(laterDeferrals, []string{"後で決めます", "あとで考える", "後で変更します", "そのあとで決める", "未定", "未定です", "未定だが縦型動画", "後ほど", "後で", "後で、決める", "主人公は後で選ぶ"}) {
		assert.True(t, declined.Deferred(s), s)
	}
	for _, s := range []string{"放課後でダンスする猫", "食後で", "卒業後で踊る", "未定義の世界", "後で見返したくなる猫"} {
		assert.False(t, declined.Deferred(s), s)
	}
}
