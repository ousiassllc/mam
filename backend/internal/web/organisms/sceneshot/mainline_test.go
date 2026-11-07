package sceneshot_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/sceneshot"
)

// #1108: Shots that stay PLANNING with no Take are not silent: the view says what the main line waits for, what is missing and what to press.
func TestView_SaysWhyTheMainLineWaitsAndWhatToPress(t *testing.T) {
	d := data()
	d.Wait = mainline.Wait{Shots: 9, Counted: true}

	html := renderHTML(t, sceneshot.View(d))

	assert.Contains(t, html, "本線は Shot の生成開始を待っています")
	assert.Contains(t, html, "Shot 9 件が PLANNING")
	assert.Contains(t, html, "「生成の承認」は出さず")
	assert.Contains(t, html, "Reference Library が 0 件")
	assert.Contains(t, html, "次に押すもの")
}

func TestView_HasNoWaitNoticeWhenNothingWaits(t *testing.T) {
	html := renderHTML(t, sceneshot.View(data()))

	assert.NotContains(t, html, "本線は Shot の生成開始を待っています")
}

// The generation test and the main line differ, and the view says which Shots the generation test results do not land on.
func TestView_TellsTheGenerationTestApartFromTheMainLine(t *testing.T) {
	html := renderHTML(t, sceneshot.View(data()))

	assert.Contains(t, html, `id="gentest-vs-main-line"`)
	assert.Contains(t, html, "生成テスト（/generation-test）の結果は")
	assert.Contains(t, html, "ここの Shot にも載りません")
}

// The wait of the main line says which phase the generation approval is in, so that a screen at FULL AUTO is not read as stuck (spec/spec.md §4.3).
func TestView_SaysWhichPhaseTheGenerationApprovalIsIn(t *testing.T) {
	for phase, wants := range map[mainline.Phase][]string{
		mainline.PhaseApproval: {`data-phase="approval"`, "生成の承認を待っています", "承認するまで Reference Pack・Keyframe・Video は一切始まりません", "FULL AUTO でも、この承認は必ず求めます"},
		mainline.PhaseDeclined: {`data-phase="declined"`, "差し戻された", "計画を直す"},
		mainline.PhaseAuto:     {`data-phase="auto"`, "自動で進めています"},
		mainline.PhaseManual:   {`data-testid="main-line-wait"`, "「生成の承認」は出さず"},
	} {
		d := data()
		d.Wait = mainline.Wait{Shots: 3, Counted: true, Phase: phase}
		html := renderHTML(t, sceneshot.View(d))
		for _, want := range wants {
			assert.Contains(t, html, want, string(phase))
		}
	}
}
