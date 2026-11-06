package mainline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
)

func TestWait_SaysWhatIsWaitedForAndWhatToPressNext(t *testing.T) {
	assert.False(t, mainline.Wait{}.Active())

	w := mainline.Wait{Shots: 9, Counted: true}
	assert.True(t, w.Active())
	text := w.Text()
	assert.Contains(t, text, "Shot 9 件が PLANNING")
	assert.Contains(t, text, "自動では始まりません")
	assert.Contains(t, text, "確認レベルは脚本・Rough Cut・Final の承認カードだけを決め")
	assert.Contains(t, text, "Reference Library が 0 件")
	assert.Contains(t, text, "Character）が 0 人")
	assert.Contains(t, text, "次に押すもの")
	assert.Contains(t, text, "「生成」")
	assert.NotContains(t, text, "モデルが悪")

	known := mainline.Wait{Shots: 1, Counted: true, References: 2, Characters: 1}
	assert.Empty(t, known.Gaps())
	assert.NotContains(t, known.Text(), "0 件")
	assert.Empty(t, mainline.Wait{Shots: 1}.Gaps(), "an uncounted Library is not reported as empty")
}

// #1108: a Subject left to the AI is not an answer (the script would be about a topic the user never named); the user's own words in
// Additional Instruction are.
func TestBrief_SubjectIsNeverLeftToTheAI(t *testing.T) {
	b := domain.Brief{ContentType: "商品CM", Objective: "TikTok", Target: "20代", Duration: "15秒", AspectRatio: "9:16", Mood: "ポップ"}
	b.Delegate(domain.BriefSubject)
	assert.Equal(t, []domain.BriefField{domain.BriefSubject}, b.Missing())
	assert.False(t, domain.BriefSubject.CanDelegate())
	assert.True(t, domain.BriefMood.CanDelegate())

	b.AdditionalInstruction = "  \n "
	assert.Contains(t, b.Missing(), domain.BriefSubject, "blank text names nothing")
	b.AdditionalInstruction = "夜の街を走る自転車の動画にする"
	assert.Empty(t, b.Missing())

	b = domain.Brief{}
	b.Set(domain.BriefSubject, "美容液")
	assert.NotContains(t, b.Missing(), domain.BriefSubject)
}
