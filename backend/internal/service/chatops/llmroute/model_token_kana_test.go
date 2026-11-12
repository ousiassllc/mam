package llmroute

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// hasModelToken counts a katakana run of 2 or more characters from the class 「\p{Katakana}ーｰﾞﾟ」 as spec §3.2 says: half-width katakana (「ﾐｽﾄﾗﾙ」), the half-width long vowel mark 「ｰ」
// and the voiced marks 「ﾞ」「ﾟ」 are in the run like the full-width 「ー」, so they do not cut it; and a run of the long vowel mark alone ("ーー", "ｰｰ") is a token.
func TestHasModelToken_KatakanaRuns(t *testing.T) {
	for name, want := range map[string]bool{
		// full-width katakana: a run of 2 or more characters, the long vowel mark counts as one
		"速い ミストラル": true,
		"速い ラマ":    true,
		"速い ラー":    true,
		"速い ーラ":    true,
		"速い ミ":     false,
		"速い ラ ー":   false, // the run is cut at a space
		// half-width katakana, 「ｰ」 and the voiced marks are inside the run, as the full-width ones are
		"速い ﾐｽﾄﾗﾙ":   true,
		"速い ﾗﾏ":      true,
		"速い ﾗｰ":      true, // 「ﾗｰ」 is a run of two, as 「ラー」 is
		"速い ﾊﾞｰｼﾞｮﾝ": true, // one run, not cut at 「ﾞ」 or 「ｰ」
		"速い ﾊﾞ":      true, // 「ﾊ」 and 「ﾞ」 are a run of two
		"速い ﾞﾟ":      true,
		"速い ﾗ":       false,
		"速い ﾗ ｰ":     false,
		// a run of the long vowel mark alone is a token, full-width or half-width; one character is not
		"速い ー":   false,
		"速い ーー":  true,
		"速い ーーー": true,
		"速い ｰ":   false,
		"速い ｰｰ":  true,
		"速い ﾞﾞ":  true,
		// an ASCII token stands beside the marks
		"速い ーー Gemma": true,
		"速い ｰｰ R1":    true,
		"速い ｰ R1":     true,
	} {
		assert.Equal(t, want, hasModelToken(name), name)
	}
}

// A degree word takes a name away from the models unless a model token stands in it; a name that holds no ASCII letter, digit or katakana ("ーー", "ｰｰ") is none either.
func TestModelLike_KatakanaBesideADegreeWord(t *testing.T) {
	for name, want := range map[string]bool{
		"速い ミストラル": true,
		"速い ﾐｽﾄﾗﾙ": true,
		"ﾐｽﾄﾗﾙ":    true,
		"速い ﾗｰ":    true,
		"速い ーー":    false,
		"速い ｰｰ":    false,
		"3 回 ーー":   true, // the 「3」 is an ASCII digit and 「ーー」 is a token
		"3 回 ｰｰ":   true, // the 「3」 is an ASCII digit and 「ｰｰ」 is a token, as 「ーー」 is
	} {
		assert.Equal(t, want, modelLike(name), name)
	}
}

func TestDetect_HalfWidthKatakanaIsASecondRoute(t *testing.T) {
	const first = "Director の LLM を Claude Opus にして、"
	assert.Empty(t, detect(first+"Producer は 速い ﾐｽﾄﾗﾙ にして"), "half-width katakana with a degree word is a model's name: left to the model")
	assert.Empty(t, detect(first+"Producer は 速い ﾗｰ にして"), "「ﾗｰ」 is a run of two, like 「ラー」: a model's name, left to the model")
	assert.Empty(t, detect(first+"Producer は 速い ラー にして"), "the full-width 「ラー」 reads the same")
	assert.Empty(t, detect(first+"Producer は 速い ﾊﾞｰｼﾞｮﾝ にして"), "「ﾊﾞｰｼﾞｮﾝ」 is one run")
	assert.Len(t, detect(first+"Producer は 速い ーー にして"), 1, "「ーー」 holds no ASCII letter, digit or katakana")
}
