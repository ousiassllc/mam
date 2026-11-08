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

// The main line says what the automatic start does (spec/spec.md §4.3): AUTO starts by itself and asks nothing; a card of v0.1.34 that was declined holds the
// start back and says how to lift it; the Shots the Orchestrator cannot carry on are the user's to start.
func TestWait_SaysWhichPhaseTheAutomaticStartIsIn(t *testing.T) {
	auto := mainline.Wait{Shots: 2, Phase: mainline.PhaseAuto}
	assert.Contains(t, auto.Text(), "承認を待たずに")
	assert.NotContains(t, auto.Text(), "承認するまで", "AUTO waits for no approval")
	assert.NotContains(t, auto.Text(), "進められません")
	assert.Equal(t, "自動開始待ち", auto.ShotLabel())
	assert.Contains(t, auto.NextAction(), mainline.BudgetCeilingScope, "a ceiling does not count an unknown price: the text says so")
	assert.Contains(t, auto.NextAction(), "DIRECTOR")

	stuck := mainline.Wait{Shots: 2, Stuck: 1, Phase: mainline.PhaseAuto}
	assert.Contains(t, stuck.Text(), "うち 1 件は自動では進められません")
	assert.Contains(t, stuck.NextAction(), "押してください")

	declined := mainline.Wait{Shots: 3, Phase: mainline.PhaseDeclined}
	assert.Contains(t, declined.Text(), "自動では始めません")
	assert.Contains(t, declined.NextAction(), "メッセージを送ってください")
	assert.Contains(t, declined.NextAction(), "計画を直す")
	assert.Contains(t, mainline.DeclinedAck, "メッセージを送ってください")
	assert.NotEqual(t, auto.ShotLabel(), declined.ShotLabel())

	unrecorded := mainline.Wait{Shots: 3, Phase: mainline.PhaseDeclinedUnrecorded}
	assert.Contains(t, unrecorded.NextAction(), "メッセージを送ってください")
	assert.NotContains(t, unrecorded.NextAction(), "計画を直す（", "a decline with no record is not lifted by the plan")
	assert.Contains(t, unrecorded.NextAction(), "自分で始めるなら、Shot を選んで手動で起動してください", "the manual start is offered, as the reply says")
	assert.Contains(t, mainline.DeclinedAckUnrecorded, "メッセージを送ってください")
	assert.NotContains(t, mainline.DeclinedAckUnrecorded, "計画（")
	assert.Equal(t, declined.ShotLabel(), unrecorded.ShotLabel())
	assert.Equal(t, declined.Title(), unrecorded.Title())

	assert.Equal(t, "生成待ち", mainline.Wait{Shots: 1}.ShotLabel(), "the manual wait keeps its words")
}
