package editor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/editor"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestRoughCut_LaysSelectedTakesOutWithFadesBetweenScenes(t *testing.T) {
	got := editor.RoughCut([]editor.ShotCut{
		{ShotID: "s1", TakeID: "t1", SceneIndex: 0, Planned: 4, TakeSeconds: 5},
		{ShotID: "s2", TakeID: "t2", SceneIndex: 0, Planned: 3, TakeSeconds: 2}, // the Take is shorter than planned
		{ShotID: "s3", TakeID: "t3", SceneIndex: 1, Planned: 0, TakeSeconds: 6}, // no plan: the whole Take
	})

	require.Len(t, got.Clips, 3)
	assert.Equal(t, 4.0, got.Clips[0].Out)
	assert.Equal(t, 2.0, got.Clips[1].Out)
	assert.Equal(t, 6.0, got.Clips[2].Out)
	assert.Equal(t, domain.Transition{Type: domain.TransitionCut}, got.Clips[0].Transition)
	assert.Equal(t, domain.Transition{Type: domain.TransitionCut}, got.Clips[1].Transition, "same Scene: a hard cut")
	assert.Equal(t, domain.Transition{Type: domain.TransitionFade, Seconds: domain.DefaultTransitionSeconds}, got.Clips[2].Transition)
	_, err := got.Normalize()
	assert.NoError(t, err)
}

type fakeLLM struct {
	answer string
	err    error
	calls  int
	agent  llm.Agent
}

func (f *fakeLLM) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	f.calls++
	f.agent = agent
	return llm.Response{Text: f.answer}, f.err
}

func judgeInput() editor.JudgeInput {
	return editor.JudgeInput{
		Brief: domain.Brief{Duration: "10秒"},
		Timeline: domain.EditTimeline{Clips: []domain.EditClip{
			{ShotID: "s1", TakeID: "t1", In: 0, Out: 5, Transition: domain.Transition{Type: domain.TransitionCut}},
			{ShotID: "s2", TakeID: "t2", In: 0, Out: 5, Transition: domain.Transition{Type: domain.TransitionCut}},
		}},
		Shots: map[string]editor.ShotFacts{
			"s1": {Label: "Shot 1", SelectedTakeID: "t1", TakeReady: true},
			"s2": {Label: "Shot 2", SelectedTakeID: "t2", TakeReady: true},
		},
	}
}

func TestJudge_PassesACutThatFitsWithoutAModel(t *testing.T) {
	verdict, findings, err := editor.NewJudge(nil).Judge(context.Background(), judgeInput())

	require.NoError(t, err)
	assert.Equal(t, domain.EditorPass, verdict)
	assert.Empty(t, findings)
}

func TestJudge_RuleFindings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*editor.JudgeInput)
		want   string
		clip   int
	}{
		{"take changed since the edit", func(in *editor.JudgeInput) {
			f := in.Shots["s2"]
			f.SelectedTakeID = "t9"
			in.Shots["s2"] = f
		}, "採用 Take が編集後に変わっています", 2},
		{"take file gone", func(in *editor.JudgeInput) {
			f := in.Shots["s1"]
			f.TakeReady = false
			in.Shots["s1"] = f
		}, "ファイルが見つかりません", 1},
		{"unknown shot", func(in *editor.JudgeInput) { delete(in.Shots, "s2") }, "Shot が Project にありません", 2},
		{"too short for the Brief", func(in *editor.JudgeInput) { in.Timeline.Clips[1].Out = 2 }, "Brief の 10.0 秒", 0},
		{"too long for the Brief", func(in *editor.JudgeInput) { in.Brief.Duration = "5秒" }, "Brief の 5.0 秒", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := judgeInput()
			tc.mutate(&in)

			verdict, findings, err := editor.NewJudge(nil).Judge(context.Background(), in)

			require.NoError(t, err)
			assert.Equal(t, domain.EditorReEdit, verdict)
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Issue, tc.want)
			assert.Equal(t, tc.clip, findings[0].ClipIndex)
			assert.NotEmpty(t, findings[0].Action)
		})
	}
}

func TestJudge_BriefWithoutADurationIsNotCheckedForLength(t *testing.T) {
	in := judgeInput()
	in.Brief.Duration = ""
	in.Timeline.Clips[1].Out = 1

	verdict, _, err := editor.NewJudge(nil).Judge(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, domain.EditorPass, verdict)
}

func TestJudge_ModelReviewAddsFindings(t *testing.T) {
	m := &fakeLLM{answer: `{"verdict":"re_edit","findings":[{"clip_index":2,"issue":"冒頭のフックが弱い","action":"クリップ 2 を先頭へ"},{"clip_index":9,"issue":"範囲外","action":"x"},{"clip_index":1,"issue":"","action":"空"}]}`}

	verdict, findings, err := editor.NewJudge(m).Judge(context.Background(), judgeInput())

	require.NoError(t, err)
	assert.Equal(t, domain.EditorReEdit, verdict)
	require.Len(t, findings, 1, "findings outside the timeline or without an issue are dropped")
	assert.Equal(t, 2, findings[0].ClipIndex)
	assert.Equal(t, llm.AgentEditor, m.agent)
}

func TestJudge_ModelPassKeepsRuleFindings(t *testing.T) {
	m := &fakeLLM{answer: `{"verdict":"pass","findings":[]}`}
	in := judgeInput()
	in.Timeline.Clips[1].Out = 2

	verdict, findings, err := editor.NewJudge(m).Judge(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, domain.EditorReEdit, verdict, "a model cannot overrule a failed rule")
	assert.Len(t, findings, 1)
}

func TestJudge_ModelNeedShotIsNotAnM1Verdict(t *testing.T) {
	m := &fakeLLM{answer: `{"verdict":"need_shot","findings":[{"clip_index":1,"issue":"Reaction Shot が足りない","action":"追加撮影"}]}`}

	verdict, _, err := editor.NewJudge(m).Judge(context.Background(), judgeInput())

	require.NoError(t, err)
	assert.Equal(t, domain.EditorReEdit, verdict, "material shortage is M3: the finding is handed back as a re-edit")
}

func TestJudge_UnusableModelAnswersAreErrors(t *testing.T) {
	for name, answer := range map[string]string{
		"not json":            "OK です",
		"unknown verdict":     `{"verdict":"maybe","findings":[]}`,
		"re_edit no findings": `{"verdict":"re_edit","findings":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := editor.NewJudge(&fakeLLM{answer: answer}).Judge(context.Background(), judgeInput())

			assert.ErrorIs(t, err, llm.ErrInvalidOutput)
		})
	}
	t.Run("provider failure", func(t *testing.T) {
		boom := errors.New("boom")

		_, _, err := editor.NewJudge(&fakeLLM{err: boom}).Judge(context.Background(), judgeInput())

		assert.ErrorIs(t, err, boom)
	})
}
