package declined_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Regressions of 1b82f272 (#1114): a vague or refused text became the Subject where the earlier head asked again.

// A deferral in a later sentence is set aside only when it positively names another matter (declined: a closed list of non-Subject objects). Any other one,
// a vague word or none ("ネタは後で考える", "後で決める", "後で猫にする"), may be the Subject put off: the answer is asked again (#1114, regression of 1b82f272).
func TestApplyAnswer_AVagueDeferralInALaterSentenceVetoesTheAnswer(t *testing.T) {
	for _, text := range []string{
		"動画を作りたい。ネタは後で考える", "動画を作りたい。内容は後で", "動画を作りたい。後で決める", "動画を作りたい。後で猫にする", "動画を作りたい。どんな動画かは後で",
		"ショート動画を作りたい。内容は後で決める",
		"猫の動画を作りたい。ネタは後で考える", "猫の動画を作りたい。内容は後で", "猫の動画を作りたい。後で決める", "猫の動画を作りたい。後で犬にする",
		// a clause that names another matter does not cover the one that does not
		"猫の動画を作りたい。尺は30秒、後で決める",
	} {
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

// With only a bare request in the first sentence nothing is left once the deferral of another matter is set aside: the answer is asked again too.
func TestApplyAnswer_ABareRequestWithADeferralOfAnotherMatterIsAskedAgain(t *testing.T) {
	for _, text := range []string{"動画を作りたい。後で見返したい", "ショート動画を作りたい。後で詳細を決める", "動画を作りたい。BGMは後で決める"} {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			assert.NotNil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Empty(t, b.Subject)
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

// A refusal that is no relative clause of its own ("作りたくない猫", "作らない人物", "出さない猫") is a refusal of the noun that follows: it is no Subject, in every path
// that reads one. Only a clause that has its own object or topic in front of the verb ("暴力を出さない猫") is a description of the noun (#1114, regression of 1b82f272).
// Changed from the earlier round: "出さない猫を主人公にして", "題材は出さない猫" and "主人公は出さない猫" gave 猫 and now give nothing (no object in front of the verb).
var bareRefusals = []string{
	"題材は作りたくない猫", "作りたくない猫", "作らない人物", "作りたくない人物", "作らないキャラ", "作らない動物", "作らないロボット", "作らないAI", "作らない猫以外",
	"作りません猫", "出さない猫", "出しません人物", "題材は出さない猫", "主人公は出さない猫", "出さない猫を主人公にして", "作らない猫の動画", "主人公は作りたくない猫", "出さない人の動画",
}

func TestExtract_ABareRefusalInFrontOfANounStatesNoSubject(t *testing.T) {
	for _, text := range bareRefusals {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

func TestApplyAnswer_ABareRefusalInFrontOfANounIsAskedAgain(t *testing.T) {
	for _, text := range bareRefusals {
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

// What only asks for a video in general ("動画を作りたい") is no Subject, even when it is what is left of a refusal ("暴力は出さない方向で動画を作りたい"): the answer is asked
// again. Changed from the earlier round: the standalone "動画を作りたい" was accepted as the answer and is now asked again (the safe direction).
var bareRequests = []string{
	"暴力は出さない方向で動画を作りたい", "動画を作りたい", "ショート動画を作りたい", "動画を作ってください", "動画をお願いします", "CMを作りたい", "映像を撮りたい", "ビデオを撮って",
	"作品を作成したい", "動画が欲しい", "お酒は出さない予定でショート動画を作って", "暴力は出さない形で動画を作りたい",
}

func TestApplyAnswer_ABareRequestIsAskedAgain(t *testing.T) {
	for _, text := range bareRequests {
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

func TestExtract_ABareRequestStatesNoSubject(t *testing.T) {
	for _, text := range bareRequests {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

// A noun next to the request is still the Subject.
func TestExtract_ANounNextToARequestIsStillTheSubject(t *testing.T) {
	for text, want := range map[string]string{
		"猫の動画を作りたい":           "猫",
		"暴力は出さない方向で猫の動画を作りたい": "猫",
		"宣伝を出さない猫を主人公にした動画":   "猫", // changed from 暴力を…: a safety word is never cut (safety_test.go)
	} {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, briefing.RuleExtractor{}.Extract(text).Subject)
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.NotEmpty(t, b.Subject)
		})
	}
}
