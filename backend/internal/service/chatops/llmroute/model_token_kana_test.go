package llmroute

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// hasModelToken reads half-width katakana and the long vowel mark as spec §3.2 says (#1145): half-width katakana is katakana, 「ー」「ｰ」 and the voiced marks 「ﾞ」「ﾟ」 belong to the run
// they stand in, and a run of those marks alone holds no katakana, so it is no token.
func TestHasModelToken_HalfWidthKatakanaAndTheLongVowelMark(t *testing.T) {
	for name, want := range map[string]bool{
		// full-width katakana: a run of 2 or more characters, the long vowel mark counts as one
		"速い ミストラル": true,
		"速い ラマ":    true,
		"速い ラー":    true,
		"速い ーラ":    true,
		"速い ミ":     false,
		"速い ラ ー":   false, // the run is cut at a space
		// half-width katakana is katakana
		"速い ﾐｽﾄﾗﾙ":   true,
		"速い ﾗﾏ":      true,
		"速い ﾗｰ":      true,
		"速い ﾊﾞｰｼﾞｮﾝ": true,
		"速い ﾗ":       false,
		"速い ﾗ ｰ":     false,
		// a run of the long vowel mark alone holds no katakana
		"速い ー":   false,
		"速い ーー":  false,
		"速い ーーー": false,
		"速い ｰｰ":  false,
		"速い ﾞﾞ":  false,
		"ーー":     false,
		"ー ー":    false,
		// the mark beside an ASCII word or a hiragana word does not make a token
		"速い A ーー": false,
		"速い ひらーー": false,
		// an ASCII token still stands beside the marks
		"速い ーー Gemma": true,
		"速い ーー R1":    true,
	} {
		assert.Equal(t, want, hasModelToken(name), name)
	}
}

// A degree word takes a name away from the models unless a model token stands in it: half-width katakana is a token as the full-width is, and 「ーー」 is not.
func TestModelLike_HalfWidthKatakanaIsAModelsNameBesideADegreeWord(t *testing.T) {
	for name, want := range map[string]bool{
		"速い ミストラル": true,
		"速い ﾐｽﾄﾗﾙ": true,
		"ﾐｽﾄﾗﾙ":    true,
		"速い ﾗｰ":    true,
		"速い ーー":    false,
		"速い ｰｰ":    false,
		"3 回 ーー":   false,
	} {
		assert.Equal(t, want, modelLike(name), name)
	}
}

func TestDetect_HalfWidthKatakanaIsASecondRouteAndTheMarkAloneIsNot(t *testing.T) {
	const first = "Director の LLM を Claude Opus にして、"
	assert.Empty(t, detect(first+"Producer は 速い ﾐｽﾄﾗﾙ にして"), "half-width katakana with a degree word is a model's name: left to the model")
	assert.Empty(t, detect(first+"Producer は 速い ﾗｰ にして"))
	assert.Len(t, detect(first+"Producer は 速い ーー にして"), 1, "a run of the long vowel mark is no model's name")
}
