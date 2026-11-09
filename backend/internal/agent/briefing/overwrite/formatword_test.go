package overwrite_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A word of the form of the video (a length, an aspect ratio, a kind, a platform, a style, a Mood) is no Subject, in front of "の動画" or as the side that is meant
// of "A ではなく B": the Subject stays unasked-for and unchanged (#1114, review of the whole PR).
var formatNouns = []string{
	"30秒", "縦型", "15秒のCM", "16:9", "1分", "短め", "ゆるい雰囲気", "TikTok", "15秒くらい", "約15秒", "15秒程度", "1分前後", "30秒以内", "3分ぐらい", "縦型動画", "レトロ風", "感動系", "アニメ風",
	"TikTok用", "TikTok向け", "YouTube用", "スマホ用", "SNS向け", "Web広告", "商品紹介動画", "15秒の縦型動画",
}

func TestAbsorb_AFormatWordInFrontOfNoDougaIsNoSubjectOfAFirstRequest(t *testing.T) {
	for _, f := range formatNouns {
		text := f + "の動画を作りたい"
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Empty(t, b.Subject, "the Subject is still to be asked: %+v", b)
			assert.Empty(t, b.ProposedSubject)
		})
	}
}

func TestAbsorb_AFormatWordNeverContradictsOrReplacesTheDecidedSubject(t *testing.T) {
	for _, f := range formatNouns {
		for _, text := range []string{f + "の動画にして", "猫ではなく" + f + "の動画にして"} {
			t.Run(text, func(t *testing.T) {
				b := decided("猫", "")
				briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
				assert.Equal(t, "猫", b.Subject)
				assert.Empty(t, b.ProposedSubject, "no card for a format word: %+v", b)
			})
		}
	}
}

// A Subject that is a thing is still read in front of "の動画".
func TestAbsorb_ASubjectInFrontOfNoDougaIsStillRead(t *testing.T) {
	for text, want := range map[string]string{"猫の動画を作りたい": "猫", "新商品のCMを作りたい": "新商品", "猫ではなく犬の動画にして": "犬"} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			if want == "猫" || want == "新商品" {
				b = domain.Brief{}
			}
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, want, b.Subject)
		})
	}
}

// A Subject that merely CONTAINS a word of a Mood, a Target, a style or an item name is a Subject: only a phrase with nothing concrete left once those words are taken away
// is not ("かわいい猫" is, "夜" and "ゆるい雰囲気" are not). Said first, it is not asked again; said as a change, it is the Subject (review of the format-word rule).
var subjectsWithKeywords = []string{
	"かわいい猫", "高級車", "ファミリーレストラン", "学生寮", "夜景", "明るい未来", "温かいスープ", "ミニマルなデスク", "ナチュラルコスメ", "長い髪の女性", "高級時計", "ゆっくり歩く犬",
}

func TestAbsorb_ASubjectThatContainsAKeywordIsStillASubject(t *testing.T) {
	for _, v := range subjectsWithKeywords {
		if strings.Contains(v, "の") {
			continue // a phrase with a の in it is not read in front of "の動画" (the noun is the last piece): a limit that was there before
		}
		t.Run(v, func(t *testing.T) {
			var b domain.Brief
			text := v + "の動画を作りたい"
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, v, b.Subject, "stated once, not asked again: %+v", b)
		})
	}
}

func TestAbsorb_AContrastToASubjectThatContainsAKeywordReplacesOrProposesIt(t *testing.T) {
	for _, v := range subjectsWithKeywords {
		for _, tail := range []string{"の動画にして", "のCMにして"} {
			text := "猫ではなく" + v + tail
			t.Run(text, func(t *testing.T) {
				b := decided("猫", "")
				briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
				assert.True(t, b.Subject == v || b.ProposedSubject == v, "replaced or proposed, never lost: S=%q P=%q", b.Subject, b.ProposedSubject)
			})
		}
	}
}

// The clause of a length, a ratio or a platform after the change is another item on a free message too, as it is on a card answer.
func TestAbsorb_AContrastWithAMeasureClauseAfterItReplacesTheSubject(t *testing.T) {
	for _, text := range []string{
		"猫ではなく犬にして、30秒で", "猫じゃなくて犬にして、縦型で", "猫ではなく犬にして、TikTokで",
	} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "犬", b.Subject)
		})
	}
}

// A clause that holds more than the bare item ("30秒くらいで", "約30秒で", "Instagram向けで") is not stripped when a decided Subject is judged (the safe side): the Subject
// is kept and 犬 is proposed in one card, never lost. On an empty Brief the clause is no part of the Subject: it is 犬.
func TestAbsorb_AHedgedMeasureClauseAfterAContrastNeverEndsUpInTheSubject(t *testing.T) {
	for _, tail := range []string{"30秒くらいで", "約30秒で", "30秒以内で", "Instagram向けで", "スマホ向けで", "YouTube用で"} {
		text := "猫ではなく犬にして、" + tail
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
			assert.Equal(t, "犬", b.ProposedSubject)
			var e domain.Brief
			briefing.Absorb(&e, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "犬", e.Subject)
		})
	}
}
