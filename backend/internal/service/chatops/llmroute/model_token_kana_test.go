package llmroute

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// hasModelToken counts a katakana run of 2 or more characters from the class 「\p{Katakana}ー」 as spec §3.2 says: half-width katakana (「ﾐｽﾄﾗﾙ」) is in the class, but the half-width long
// vowel mark 「ｰ」 and the voiced marks 「ﾞ」「ﾟ」 are not, so they end a run; and a run of the full-width 「ー」 alone ("ーー") is a token.
func TestHasModelToken_KatakanaRuns(t *testing.T) {
	for name, want := range map[string]bool{
		// full-width katakana: a run of 2 or more characters, the long vowel mark counts as one
		"速い ミストラル": true,
		"速い ラマ":    true,
		"速い ラー":    true,
		"速い ーラ":    true,
		"速い ミ":     false,
		"速い ラ ー":   false, // the run is cut at a space
		// half-width katakana is katakana, but 「ｰ」 and the voiced marks are outside the run
		"速い ﾐｽﾄﾗﾙ":   true,
		"速い ﾗﾏ":      true,
		"速い ﾗｰ":      false, // 「ﾗ」 is a run of one
		"速い ﾊﾞｰｼﾞｮﾝ": true,  // 「ｮﾝ」 is a run of two
		"速い ﾗ":       false,
		"速い ﾗ ｰ":     false,
		// a run of the full-width long vowel mark alone is a token; the half-width one is not
		"速い ー":   false,
		"速い ーー":  true,
		"速い ーーー": true,
		"速い ｰｰ":  false,
		"速い ﾞﾞ":  false,
		// an ASCII token stands beside the marks
		"速い ーー Gemma": true,
		"速い ｰｰ R1":    true,
	} {
		assert.Equal(t, want, hasModelToken(name), name)
	}
}

// A degree word takes a name away from the models unless a model token stands in it; a name that holds no ASCII letter, digit or katakana ("ーー") is none either.
func TestModelLike_KatakanaBesideADegreeWord(t *testing.T) {
	for name, want := range map[string]bool{
		"速い ミストラル": true,
		"速い ﾐｽﾄﾗﾙ": true,
		"ﾐｽﾄﾗﾙ":    true,
		"速い ﾗｰ":    false,
		"速い ーー":    false,
		"速い ｰｰ":    false,
		"3 回 ーー":   true, // the 「3」 is an ASCII digit and 「ーー」 is a token
		"3 回 ｰｰ":   false,
	} {
		assert.Equal(t, want, modelLike(name), name)
	}
}

func TestDetect_HalfWidthKatakanaIsASecondRoute(t *testing.T) {
	const first = "Director の LLM を Claude Opus にして、"
	assert.Empty(t, detect(first+"Producer は 速い ﾐｽﾄﾗﾙ にして"), "half-width katakana with a degree word is a model's name: left to the model")
	assert.Len(t, detect(first+"Producer は 速い ﾗｰ にして"), 1, "「ﾗｰ」 holds no run of two")
	assert.Len(t, detect(first+"Producer は 速い ーー にして"), 1, "「ーー」 holds no ASCII letter, digit or katakana")
}
