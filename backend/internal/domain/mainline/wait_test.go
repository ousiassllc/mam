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
	assert.Contains(t, text, "「生成の承認」は出さず")
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

// The generation approval changes what the main line says (spec/spec.md §4.3): an approval that is awaited says nothing starts before it and that
// the answer carries the Shots on by itself, even at FULL AUTO; a decline says what to do next; an approved plan says it is carried on and which
// Shots the user must start.
func TestWait_SaysWhichPhaseTheGenerationApprovalIsIn(t *testing.T) {
	asking := mainline.Wait{Shots: 3, Phase: mainline.PhaseApproval}
	text := asking.Text()
	assert.Contains(t, text, "生成の承認を待っています")
	assert.Contains(t, text, "承認するまで Reference Pack・Keyframe・Video は一切始まりません")
	assert.Contains(t, text, "Reference Pack → Shot 生成が自動で進みます")
	assert.Contains(t, text, "FULL AUTO でも、この承認は必ず求めます")
	assert.Equal(t, "承認待ち", asking.ShotLabel())
	assert.Contains(t, asking.Title(), "生成の承認")

	declined := mainline.Wait{Shots: 3, Phase: mainline.PhaseDeclined}
	assert.Contains(t, declined.Text(), "差し戻された")
	assert.Contains(t, declined.Text(), "生成は始まりません")
	assert.Contains(t, declined.Text(), "計画を直す")
	assert.Contains(t, declined.Text(), "メッセージを送ってください")
	assert.Equal(t, "承認待ち", declined.ShotLabel())
	assert.Contains(t, mainline.DeclinedAck, "③そのままもう一度承認を求めるには")

	auto := mainline.Wait{Shots: 2, Phase: mainline.PhaseAuto}
	assert.Contains(t, auto.Text(), "自動で進めています")
	assert.NotContains(t, auto.Text(), "進められません")
	assert.Equal(t, "自動開始待ち", auto.ShotLabel())

	stuck := mainline.Wait{Shots: 2, Stuck: 1, Phase: mainline.PhaseAuto}
	assert.Contains(t, stuck.Text(), "うち 1 件は自動では進められません")
	assert.Contains(t, stuck.NextAction(), "押してください")

	assert.Equal(t, "生成待ち", mainline.Wait{Shots: 1}.ShotLabel(), "the manual wait keeps its words")
}
