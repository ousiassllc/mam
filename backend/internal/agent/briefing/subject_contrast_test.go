package briefing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// "A ではなく B" about another item is no Subject change, and what is denied is never read as that item's value (#1114).
func TestExtract_AContrastAboutAnotherItemIsNoSubjectAndIsReadFromThePositiveSide(t *testing.T) {
	for _, tt := range []struct {
		text    string
		subject string
		target  string
		mood    string
	}{
		{"ターゲットは学生ではなく社会人", "", "社会人", ""},
		{"ターゲットは主婦ではなく学生", "", "学生", ""},
		{"雰囲気は派手でなく地味", "", "", "地味"},
		{"雰囲気は高級ではなくポップ", "", "", "ポップ"},
		{"長さは短めじゃなくて長め", "", "", ""},
		{"学生ではなく社会人向けにして", "", "", ""},
		{"高級ではなくポップな雰囲気で", "", "", "ポップ"},
		{"商品ではなく猫の日常", "猫の日常", "", ""},
		{"猫ではなく犬の動画", "犬", "", ""},
	} {
		t.Run(tt.text, func(t *testing.T) {
			got := briefing.RuleExtractor{}.Extract(tt.text)
			assert.Equal(t, tt.subject, got.Subject)
			assert.Equal(t, tt.target, got.Target)
			assert.Equal(t, tt.mood, got.Mood)
		})
	}
}

// Against a decided Subject, a contrast about the target, the mood or the length leaves the Subject alone (no change, no contradiction)
// and the other item takes the positive side only.
func TestAbsorb_AContrastAboutAnotherItemLeavesTheDecidedSubjectAlone(t *testing.T) {
	for _, tt := range []struct {
		text   string
		target string
		mood   string
		dur    string
	}{
		{"雰囲気は派手でなく地味", "30代女性", "地味", "15秒"},
		{"長さは短めじゃなくて長め", "30代女性", "落ち着いた", "15秒"},
		{"長さは15秒ではなく30秒", "30代女性", "落ち着いた", "30秒"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			b := intake(firstRequest)
			b.Set(domain.BriefSubject, b.Subject)
			b.Set(domain.BriefTarget, "30代女性")
			subject := b.Subject

			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(tt.text), tt.text)

			assert.Equal(t, subject, b.Subject)
			assert.Empty(t, b.ProposedSubject)
			assert.Equal(t, "30代女性", b.Get(domain.BriefTarget), "a decided Target stays unless the text changes it")
			assert.Equal(t, tt.mood, b.Mood, "the denied mood is never taken")
			assert.Equal(t, tt.dur, b.Duration)
		})
	}
}

// The target changes only from the positive side of the contrast; the denied side is never read.
func TestAbsorb_ATargetContrastChangesTheTargetToThePositiveSide(t *testing.T) {
	b := intake(firstRequest)
	b.Set(domain.BriefSubject, b.Subject)
	b.Set(domain.BriefTarget, "学生")
	text := "ターゲットは学生ではなく社会人"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "社会人", b.Target)
	assert.Equal(t, "架空の30代女性インフルエンサー", b.Subject)
	assert.Empty(t, b.ProposedSubject)
}

// A denied "X が主人公" goes whole, and an answer is the noun phrase after the contrast (#1114).
func TestSubject_ADeniedProtagonistPhraseGoesWhole(t *testing.T) {
	for text, want := range map[string]string{
		"猫が主人公ではなく犬が主人公":  "犬",
		"猫が主人公じゃなくて犬が主人公": "犬",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, briefing.RuleExtractor{}.Extract(text).Subject)
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, want, b.Subject)
		})
	}
	for text, want := range map[string]string{
		"主人公は猫ではなく犬":      "犬",
		"主人公は猫じゃなくて犬です":   "犬",
		"主人公は男性ではなく女性":    "女性",
		"猫ではなく犬を主人公にして":   "犬",
		"ターゲットは学生ではなく社会人": "社会人",
	} {
		t.Run("answer "+text, func(t *testing.T) {
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, want, b.Subject)
		})
	}
}

// A refusal is read per sentence and clause: what is left of the answer is still the Subject, and a word that only contains 不要 is no refusal (#1114).
func TestApplyAnswer_ARefusalIsReadPerClause(t *testing.T) {
	for text, want := range map[string]string{
		"不要品回収のCM":        "不要品回収のCM",
		"不要品回収":           "不要品回収",
		"犬を主人公に。猫はいらない":   "犬",
		"犬を主人公に、猫は不要です":   "犬",
		"猫はいらない。犬を主人公にして": "犬",
	} {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, want, b.Subject)
		})
	}
	for _, text := range []string{"猫は不要です", "猫はいらない", "猫を主人公にしてほしくない"} {
		t.Run("refused "+text, func(t *testing.T) {
			var b domain.Brief
			assert.NotNil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Empty(t, b.Subject)
		})
	}
}
