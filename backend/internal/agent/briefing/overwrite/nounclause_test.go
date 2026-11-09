package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Only an item that can never name a Subject (a duration, an aspect ratio, a platform) is "another item" after the change. A noun (a Target, a Content Type, a Mood)
// is as good a Subject as it is another item ("学生にして" is the same shape as "犬にして"), so the evidence for the first Subject is not clear: the decided Subject stays and
// the user is asked about that one point (#1114, "確定 Subject を変える明示的な根拠がないときは保持する").
var nounClauses = []string{
	"学生にして", "学生で", "学生に", "学生がいい", "学生でお願いします", "主婦にして", "経営者にして", "ファミリーにして", "ビジネスパーソンにして",
	"アニメにして", "アニメで", "ドラマにして", "CMにして", "夜にして",
}

func TestAbsorbAndApplyAnswer_ANounClauseAfterTheChangeIsNoOtherItemSoTheSubjectStays(t *testing.T) {
	for _, tail := range nounClauses {
		text := "主人公を犬に変更して、" + tail
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject, "the decided Subject stays")
			assert.Equal(t, "犬", b.ProposedSubject, "and the user is asked about 犬")

			a := decided("猫", "")
			again := briefing.ApplyAnswer(&a, domain.BriefSubject, text)
			assert.NotNil(t, again, "asked")
			assert.Equal(t, "猫", a.Subject)
			assert.Equal(t, "犬", a.ProposedSubject)

			a = decided("猫", "ロボット")
			briefing.ApplyAnswer(&a, domain.BriefSubject, text)
			assert.Equal(t, "猫", a.Subject)
			assert.Equal(t, "ロボット", a.ProposedSubject, "an open contradiction stays as it was")
		})
	}
}

// The extractor still reads the noun as its item from a free message: it is kept, not lost.
func TestAbsorb_TheNounClauseIsStillReadAsItsItem(t *testing.T) {
	for text, check := range map[string]func(b domain.Brief) bool{
		"主人公を犬に変更して、学生にして":  func(b domain.Brief) bool { return b.Target == "学生" },
		"主人公を犬に変更して、アニメにして": func(b domain.Brief) bool { return b.ContentType == "アニメ" },
	} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.True(t, check(b), "%+v", b)
		})
	}
}

// "A ではなく B にして" followed by a noun clause is judged the same on both paths (the answer path used to replace and drop the noun, the free message kept the Subject).
func TestApplyAnswer_AContrastFollowedByANounClauseKeepsTheSubjectLikeAFreeMessage(t *testing.T) {
	text := "猫ではなく犬にして、学生にして"
	a := decided("猫", "")
	briefing.ApplyAnswer(&a, domain.BriefSubject, text)
	assert.Equal(t, "猫", a.Subject)
	b := decided("猫", "")
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "猫", b.Subject)
}

// A measure or a platform after the change is still another item.
func TestAbsorbAndApplyAnswer_AMeasureOrAPlatformClauseIsStillAnotherItem(t *testing.T) {
	for _, tail := range []string{"30秒で", "縦型で", "16:9にして", "1分30秒で", "TikTokで"} {
		text := "主人公を犬に変更して、" + tail
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			a := decided("猫", "")
			assert.Nil(t, briefing.ApplyAnswer(&a, domain.BriefSubject, text))
			assert.Equal(t, "犬", a.Subject)
			assert.Equal(t, "犬", b.Subject)
		})
	}
}

// "A ではなく B にして、学生にして": B is not lost (it is proposed, one card) and the noun clause is read as its own item; the Subject is kept on a free message and on a card answer.
func TestAbsorbAndApplyAnswer_ANounClauseAfterAContrastNeitherLosesBNorReplacesTheSubject(t *testing.T) {
	for _, tail := range []string{"学生にして", "アニメにして", "学生で", "ドラマにして"} {
		text := "猫ではなく犬にして、" + tail
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
			assert.Equal(t, "犬", b.ProposedSubject)
			c := decided("猫", "")
			assert.NotNil(t, briefing.ApplyAnswer(&c, domain.BriefSubject, text), "asked once")
			assert.Equal(t, "猫", c.Subject)
			assert.Equal(t, "犬", c.ProposedSubject)
		})
	}
}
