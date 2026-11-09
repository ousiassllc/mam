package declined_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing/declined"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The last bounded fixes of the Brief free-text reader (#1114). The reader is heuristic and open-ended; each of these fails closed.

func askedAgain(t *testing.T, text string) domain.Brief {
	t.Helper()
	var b domain.Brief
	again := briefing.ApplyAnswer(&b, domain.BriefSubject, text)
	if assert.NotNil(t, again, "asked again") {
		assert.Equal(t, string(domain.BriefSubject), again.RefID)
	}
	assert.Empty(t, b.Subject)
	return b
}

// A refusal that ends the statement in a nominalizer after the clause of the protagonist ("猫を主人公にして暴力は出さない方向で") is no relative clause of a noun: the Subject is
// the protagonist, and the refusal is not glued to it ("猫を方向で"). Regression of the own-clause cut (fix10).
func TestSubject_AProtagonistWithARefusalThatEndsInANominalizerKeepsTheSubject(t *testing.T) {
	for text, want := range map[string]string{
		"猫を主人公にして暴力は出さない方向で":     "猫",
		"猫が主人公で暴力は出さない形で":        "猫",
		"猫が主人公で暴力は出さない予定です":      "猫",
		"猫を主人公にして暴力は出さない事にして":    "猫",
		"猫を主人公にして暴力は出さない方針で":     "猫",
		"猫を主人公にして裸は出さない方向で":      "猫",
		"女性を主人公にして暴力は出さない人物":     "女性",
		"主人公は猫で暴力は出さない方向で":       "猫",
		"猫を主人公にして、暴力は出さない方向で":    "猫",
		"猫を主人公にして。暴力は出さない方向で":    "猫",
		"猫を主人公にして暴力は出さない":        "猫",
		"主人公は猫で未成年は出さない予定です":     "猫",
		"題材は猫で暴力は出さない方向で":        "猫",
		"架空の女性を主人公にして未成年は出さない予定": "架空の女性",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, briefing.RuleExtractor{}.Extract(text).Subject)
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, want, b.Subject)
		})
	}
}

// A request that runs on into a connective or a hedge ("動画を作りたいけど", "動画を作りたいと思います") says what is asked for, not what the video is about: it is no Subject.
var bareRequestsWithATail = []string{
	"動画を作りたいけど", "動画を作りたくて", "動画を作りたいんですけど", "動画を作りたいなと思っています", "動画を作りたいと思います", "動画を作りたいけど暴力は出さない",
	"動画を作りたいけれど", "動画を作りたいけれども", "動画を作りたいのですけど", "動画を作りたいなと思う", "動画を作りたいなと思ってます", "動画を撮りたくて", "動画を作りたいんです",
	"動画を作りたいです", "動画を作りたいです。", "動画を作りたいです！", "動画を撮りたいです", "動画を作成したいです", "ビデオを作りたいです", "動画を作りたいですが",
}

func TestSubject_ABareRequestWithAConnectiveOrHedgeTailIsAskedAgain(t *testing.T) {
	for _, text := range bareRequestsWithATail {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
			askedAgain(t, text)
		})
	}
}

func TestSubject_ANounInFrontOfARequestWithATailIsStillTheSubject(t *testing.T) {
	for text, want := range map[string]string{
		"猫の動画を作りたいけど":           "猫",
		"猫の動画を作りたくて":            "猫",
		"猫の動画を作りたいなと思っています":     "猫",
		"ロボットの動画を作りたいと思います":     "ロボット",
		"猫の動画を作りたいんですけど暴力は出さない": "猫",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, briefing.RuleExtractor{}.Extract(text).Subject)
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Contains(t, b.Subject, want)
		})
	}
}

// A refusal of a safety word in front of a noun ("未成年は出さない猫の動画を作りたい") is a stated constraint: it is never cut away from the Subject without trace. The clause is not cut:
// no Subject is read from the text (asked again, not a garbled one), and the refusal is kept in the Additional Instruction (a "制約: …" line).
// Changed from the earlier rounds: the relative clause with 暴力 / お酒 as the object was cut and left 猫 / 人物 / 人; now only a clause with another object is cut (宣伝を出さない猫).
var safetyRefusalsInFrontOfANoun = map[string]string{
	"未成年は出さない猫の動画を作りたい":   "未成年は出さない",
	"裸は出さない猫の動画を作りたい":     "裸は出さない",
	"実在の人物は出さない猫の動画を作りたい": "実在の人物は出さない",
	"子供を出さない猫の動画を作りたい":    "子供を出さない",
	"暴力を出さない猫の動画を作りたい":    "暴力を出さない",
	"暴力は出さない猫の動画を作りたい":    "暴力は出さない",
	"暴力を作らない猫の動画を作りたい":    "暴力を作らない",
	"暴力を出さない猫を主人公にした動画":   "暴力を出さない",
	"暴力を出さない猫が主人公":        "暴力を出さない",
	"主人公は暴力を出さない猫":        "暴力を出さない",
	"題材は暴力を出さない猫":         "暴力を出さない",
	"暴力を作りません猫を主人公にして":    "暴力を作りません",
	"暴力を出しません猫を主人公にして":    "暴力を出しません",
	"お酒を出さない人物を主人公にした動画":  "酒を出さない",
	"酒を出さない人の動画":          "酒を出さない",
	"お酒を出さない人の話を作りたい":     "酒を出さない",
	"暴力を出さない形の動画を作りたい":    "暴力を出さない",
}

func TestSubject_ASafetyRefusalInFrontOfANounIsKeptAsAConstraint(t *testing.T) {
	for text, refusal := range safetyRefusalsInFrontOfANoun {
		t.Run(text, func(t *testing.T) {
			extracted := briefing.RuleExtractor{}.Extract(text)
			assert.Empty(t, extracted.Subject)
			assert.Equal(t, briefing.ConstraintPrefix+refusal, extracted.AdditionalInstruction)

			b := askedAgain(t, text)
			assert.Equal(t, briefing.ConstraintPrefix+refusal, b.AdditionalInstruction)
		})
	}
}

// A refusal of a safety word cut at a connective ("猫の動画を作りたいが暴力は出さない") leaves the Subject the request names and is kept as a "制約: …" line, visible on the
// confirmation card and deletable.
func TestSubject_ASafetyRefusalCutAtAConnectiveIsKeptAsAConstraint(t *testing.T) {
	for _, tt := range []struct{ text, subject, instruction string }{
		{"ロボットの動画を作りたくて酒は出しません", "ロボット", "制約: 酒は出しません"},
		{"猫の動画を作りたいが暴力は出さない", "猫", "制約: 暴力は出さない"},
		{"猫の動画を作りたいけど暴力は出さない", "猫", "制約: 暴力は出さない"},
		{"猫の動画を作りたい、暴力は出さない", "猫", "制約: 暴力は出さない"},
		{"暴力は出さない方向で猫の動画を作りたい", "猫", "制約: 暴力は出さない"},
		{"猫の動画を作りたい。裸は出さない方向で", "猫", "制約: 裸は出さない"},
		{"猫を主人公にして未成年は出さない", "猫", "制約: 未成年は出さない"},
		{"猫の動画を作りたいが宣伝は出さない", "猫", ""},
	} {
		t.Run(tt.text, func(t *testing.T) {
			extracted := briefing.RuleExtractor{}.Extract(tt.text)
			assert.Equal(t, tt.subject, extracted.Subject)
			assert.Equal(t, tt.instruction, extracted.AdditionalInstruction)

			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, tt.text))
			assert.Contains(t, b.Subject, tt.subject)
			assert.Equal(t, tt.instruction, b.AdditionalInstruction)
		})
	}
}

// A refusal of something that is no safety word holds no constraint.
func TestRefusals_OfNoSafetyWordIsNoConstraint(t *testing.T) {
	for _, text := range []string{"猫の動画は作らない", "暴力は出したくないので、猫の動画は作りません"} {
		assert.Empty(t, declined.Refusals(text), text)
	}
	assert.Equal(t, []string{"暴力は出さない"}, declined.Refusals("猫の動画を作りたいが暴力は出さない"))
	assert.Equal(t, []string{"暴力は出さない", "酒は出しません"}, declined.Refusals("猫の動画を作りたい。暴力は出さない。酒は出しません"))
}

// The refusal joins the constraints already kept, after the labels, once.
func TestAbsorb_ARefusalJoinsTheConstraintLine(t *testing.T) {
	text := "猫の動画を作りたいが暴力は出さない"
	b := domain.Brief{AdditionalInstruction: "制約: 成人", Sources: map[string]string{string(domain.BriefAdditionalInstruction): domain.BriefSourceInferred}}
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "制約: 成人・暴力は出さない", b.AdditionalInstruction)
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "制約: 成人・暴力は出さない", b.AdditionalInstruction)
}
