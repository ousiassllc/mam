package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A word of the form of the video (a length, an aspect ratio, a kind, a platform, a style, a Mood) is no Subject, in front of "の動画" or as the side that is meant
// of "A ではなく B": the Subject stays unasked-for and unchanged (#1114, review of the whole PR).
var formatNouns = []string{"30秒", "縦型", "15秒のCM", "16:9", "1分", "短め", "ゆるい雰囲気", "TikTok"}

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
