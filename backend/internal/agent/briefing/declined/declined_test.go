package declined_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing/declined"
)

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
	} {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
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
	for _, s := range []string{"後で決めます", "あとで考える", "後で変更します", "そのあとで決める", "未定", "未定です", "未定だが縦型動画", "後ほど", "後で", "後で、決める", "主人公は後で選ぶ"} {
		assert.True(t, declined.Deferred(s), s)
	}
	for _, s := range []string{"放課後でダンスする猫", "食後で", "卒業後で踊る", "未定義の世界", "後で見返したくなる猫"} {
		assert.False(t, declined.Deferred(s), s)
	}
}
