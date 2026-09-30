package continuation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/continuation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type fakeLLM struct {
	answer string
	agent  llm.Agent
	user   string
}

func (f *fakeLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.agent, f.user = agent, req.User
	return llm.Response{Text: f.answer, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

func input() continuation.Input {
	return continuation.Input{
		Scene: domain.Scene{Title: "Hallway"},
		Shot:  domain.Shot{Objective: "She reaches for the handle", Duration: 5},
		World: domain.WorldContext{Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio", Emotion: "calm"}}},
	}
}

func TestPropose_KeepsAtMostThreeDistinctShortCandidatesAndClipsTheAnalysis(t *testing.T) {
	long := strings.Repeat("あ", 101)
	f := &fakeLLM{answer: `{"analysis":{"final_frame":"` + strings.Repeat("あ", 300) + `"},
		"candidates":["  A案 ","A案","` + long + `","","B案","C案","D案"]}`}

	p, err := continuation.New(f).Propose(context.Background(), input())

	require.NoError(t, err)
	assert.Equal(t, llm.AgentDirector, f.agent)
	assert.Equal(t, []string{"A案", "B案", "C案"}, p.Candidates)
	assert.Len(t, []rune(p.Analysis.FinalFrame), 200)
	assert.Contains(t, f.user, `emotion="calm"`, "the World State is part of the prompt")
}

func TestPropose_WithoutAUsableCandidateIsAnInvalidPlan(t *testing.T) {
	_, err := continuation.New(&fakeLLM{answer: `{"analysis":{},"candidates":["","  "]}`}).Propose(context.Background(), input())
	assert.ErrorIs(t, err, planning.ErrInvalidPlan)
}

func TestPlan_DurationFallsBackToTheCompletedShotAndIsCapped(t *testing.T) {
	a := continuation.New(&fakeLLM{answer: `{"new_scene":false,"shot":{"objective":" 扉を開ける "}}`})
	plan, err := a.Plan(context.Background(), input(), "")
	require.NoError(t, err)
	assert.False(t, plan.NewScene)
	assert.Equal(t, "扉を開ける", plan.Shot.Objective)
	assert.Equal(t, 5.0, plan.Shot.DurationSeconds)

	a = continuation.New(&fakeLLM{answer: `{"new_scene":false,"shot":{"objective":"x","duration_seconds":600}}`})
	plan, err = a.Plan(context.Background(), input(), "x")
	require.NoError(t, err)
	assert.Equal(t, 60.0, plan.Shot.DurationSeconds)
}

func TestPlan_ToldToChooseByItselfWhenTheDirectionIsEmpty(t *testing.T) {
	f := &fakeLLM{answer: `{"shot":{"objective":"x","duration_seconds":3}}`}
	_, err := continuation.New(f).Plan(context.Background(), input(), "")
	require.NoError(t, err)
	assert.Contains(t, f.user, "AI に任せる")

	_, err = continuation.New(f).Plan(context.Background(), input(), "扉を開ける")
	require.NoError(t, err)
	assert.Contains(t, f.user, "扉を開ける")
	assert.NotContains(t, f.user, "AI に任せる")
}

func TestPlan_RejectsAnAnswerWithoutAShotObjectiveOrANewSceneTitle(t *testing.T) {
	for _, answer := range []string{
		`{"shot":{"objective":""}}`,
		`{"new_scene":true,"scene":{"title":" "},"shot":{"objective":"x"}}`,
	} {
		_, err := continuation.New(&fakeLLM{answer: answer}).Plan(context.Background(), input(), "")
		assert.ErrorIs(t, err, planning.ErrInvalidPlan, answer)
	}
}

func TestPropose_AttachesTheFinalFrameImageWhenThereIsOne(t *testing.T) {
	var got llm.Request
	spy := llmFunc(func(req llm.Request) { got = req })
	in := input()

	_, err := continuation.New(spy).Propose(context.Background(), in)
	require.NoError(t, err)
	assert.Empty(t, got.Images, "no frame: the text alone")

	in.FinalFrame = &llm.Image{MimeType: "image/jpeg", Data: []byte{0xFF, 0xD8}}
	_, err = continuation.New(spy).Propose(context.Background(), in)
	require.NoError(t, err)
	require.Len(t, got.Images, 1)
	assert.Equal(t, []byte{0xFF, 0xD8}, got.Images[0].Data)
	assert.Contains(t, got.Images[0].Caption, "最終フレーム")
	assert.Empty(t, in.FinalFrame.Caption, "the caller's image is not modified")
}

type llmFunc func(llm.Request)

func (f llmFunc) Complete(_ context.Context, _ llm.Agent, req llm.Request) (llm.Response, error) {
	f(req)
	return llm.Response{Text: `{"analysis":{},"candidates":["A"]}`, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}
