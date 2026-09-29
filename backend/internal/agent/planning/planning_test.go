package planning_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// fakeLLM stands in for the Reasoning LLM Adapter: no external API is called. It answers
// every call with the next canned text and records what each Agent asked.
type fakeLLM struct {
	answers []string
	err     error
	agents  []llm.Agent
	users   []string
	jsonReq []bool
}

func (f *fakeLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.agents = append(f.agents, agent)
	f.users = append(f.users, req.User)
	f.jsonReq = append(f.jsonReq, req.JSON)
	if f.err != nil {
		return llm.Response{}, f.err
	}
	text := f.answers[min(len(f.agents)-1, len(f.answers)-1)]
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

func brief() domain.Brief {
	return domain.Brief{
		ContentType: "商品CM", Objective: "Instagram広告", Subject: "美容液", Target: "20代女性",
		Duration: "15秒", AspectRatio: "9:16", Delegated: []string{string(domain.BriefMood)},
	}
}

const conceptJSON = `{"title":"朝の一滴","logline":"肌が目覚める","concept":"朝の光と美容液","tone":"上品","structure":[{"name":"フック","description":"光","duration_seconds":3}]}`

func TestProducer_DevelopsConceptFromBrief(t *testing.T) {
	f := &fakeLLM{answers: []string{"```json\n" + conceptJSON + "\n```"}}
	c, err := planning.NewProducer(f).Develop(context.Background(), planning.ConceptInput{Brief: brief(), Template: domain.TemplateProductCM})
	require.NoError(t, err)
	assert.Equal(t, "朝の一滴", c.Title)
	require.Len(t, c.Structure, 1)

	assert.Equal(t, []llm.Agent{llm.AgentProducer}, f.agents)
	assert.True(t, f.jsonReq[0])
	assert.Contains(t, f.users[0], "美容液")
	assert.Contains(t, f.users[0], "AI に任せる", "a delegated field is stated as such")
	assert.NotContains(t, f.users[0], "修正依頼", "no feedback section on a first draft")
}

func TestProducer_FeedbackAndPreviousConceptReachThePrompt(t *testing.T) {
	f := &fakeLLM{answers: []string{conceptJSON}}
	prev := domain.Concept{Title: "旧案タイトル"}
	_, err := planning.NewProducer(f).Develop(context.Background(), planning.ConceptInput{Brief: brief(), Previous: &prev, Feedback: "もっと落ち着いたトーンで"})
	require.NoError(t, err)
	assert.Contains(t, f.users[0], "旧案タイトル")
	assert.Contains(t, f.users[0], "もっと落ち着いたトーンで")
}

func TestProducer_RejectsUnusableConcepts(t *testing.T) {
	for name, answer := range map[string]string{
		"no title":     `{"title":" ","concept":"c","structure":[{"name":"a"}]}`,
		"no concept":   `{"title":"t","concept":"","structure":[{"name":"a"}]}`,
		"no structure": `{"title":"t","concept":"c","structure":[]}`,
		"not json":     `sorry`,
	} {
		_, err := planning.NewProducer(&fakeLLM{answers: []string{answer}}).Develop(context.Background(), planning.ConceptInput{Brief: brief()})
		require.Error(t, err, name)
	}
	_, err := planning.NewProducer(&fakeLLM{answers: []string{`{"title":"t","concept":"c","structure":[]}`}}).Develop(context.Background(), planning.ConceptInput{})
	assert.ErrorIs(t, err, planning.ErrInvalidPlan)
}

func TestProducer_PropagatesProviderFailure(t *testing.T) {
	_, err := planning.NewProducer(&fakeLLM{err: assert.AnError}).Develop(context.Background(), planning.ConceptInput{Brief: brief()})
	assert.ErrorIs(t, err, assert.AnError)
}

const scriptJSON = `{"title":"脚本","scenes":[
  {"title":"朝","location":"寝室","time":"朝","objective":"目覚め","description":"光が差す","dialogue":["ナレーション: おはよう"]},
  {"title":"使う","description":"美容液を手に取る"}]}`

func TestWriter_WritesScriptFromConcept(t *testing.T) {
	f := &fakeLLM{answers: []string{scriptJSON}}
	s, err := planning.NewWriter(f).Write(context.Background(), planning.ScriptInput{
		Brief: brief(), Concept: domain.Concept{Title: "朝の一滴", Concept: "朝の光と美容液"},
	})
	require.NoError(t, err)
	require.Len(t, s.Scenes, 2)
	assert.Equal(t, "寝室", s.Scenes[0].Location)
	assert.Equal(t, []llm.Agent{llm.AgentWriter}, f.agents)
	assert.Contains(t, f.users[0], "朝の光と美容液", "the Concept is the input of the Writer")
}

func TestWriter_UsesPreviousScriptAndFeedback(t *testing.T) {
	f := &fakeLLM{answers: []string{scriptJSON}}
	prev := domain.Script{Title: "旧脚本"}
	_, err := planning.NewWriter(f).Write(context.Background(), planning.ScriptInput{Brief: brief(), Previous: &prev, Feedback: "Scene を 2 つに"})
	require.NoError(t, err)
	assert.Contains(t, f.users[0], "旧脚本")
	assert.Contains(t, f.users[0], "Scene を 2 つに")
}

func TestWriter_RejectsUnusableScripts(t *testing.T) {
	for name, answer := range map[string]string{
		"no scene":       `{"title":"t","scenes":[]}`,
		"no description": `{"title":"t","scenes":[{"title":"a","description":""}]}`,
		"no title":       `{"title":"t","scenes":[{"title":"","description":"d"}]}`,
	} {
		_, err := planning.NewWriter(&fakeLLM{answers: []string{answer}}).Write(context.Background(), planning.ScriptInput{Brief: brief()})
		assert.ErrorIs(t, err, planning.ErrInvalidPlan, name)
	}
}

func planJSON(durations ...float64) string {
	shots := ""
	for i, d := range durations {
		if i > 0 {
			shots += ","
		}
		shots += `{"objective":"ショット","duration_seconds":` + trim(d) + `}`
	}
	return `{"scenes":[{"title":"S1","location":"寝室","shots":[` + shots + `]}]}`
}

func trim(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func TestDirector_PlansScenesAndShots(t *testing.T) {
	f := &fakeLLM{answers: []string{planJSON(5, 5, 5)}}
	plan, err := planning.NewDirector(f).Plan(context.Background(), planning.PlanInput{
		Brief: brief(), Script: domain.Script{Title: "脚本タイトルX"}, Concept: domain.Concept{Title: "企画Y"},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, plan.ShotCount())
	assert.Equal(t, []llm.Agent{llm.AgentDirector}, f.agents)
	assert.Contains(t, f.users[0], "脚本タイトルX", "the Script is the input of the Director")
}

func TestDirector_RescalesShotDurationsToTheBriefDuration(t *testing.T) {
	// 30 s planned for a 15 s brief.
	plan, err := planning.NewDirector(&fakeLLM{answers: []string{planJSON(10, 10, 10)}}).Plan(context.Background(), planning.PlanInput{Brief: brief()})
	require.NoError(t, err)
	total := 0.0
	for _, sh := range plan.Scenes[0].Shots {
		total += sh.DurationSeconds
	}
	assert.InDelta(t, 15, total, 0.3)
	assert.InDelta(t, 5, plan.Scenes[0].Shots[0].DurationSeconds, 0.05)
}

func TestDirector_KeepsDurationsWithinTolerance(t *testing.T) {
	plan, err := planning.NewDirector(&fakeLLM{answers: []string{planJSON(5, 5.5, 4.8)}}).Plan(context.Background(), planning.PlanInput{Brief: brief()})
	require.NoError(t, err)
	assert.Equal(t, 5.5, plan.Scenes[0].Shots[1].DurationSeconds, "15.3 s is close enough to 15 s")
}

func TestDirector_LeavesDurationsAloneWhenBriefHasNone(t *testing.T) {
	b := brief()
	b.Duration = ""
	plan, err := planning.NewDirector(&fakeLLM{answers: []string{planJSON(10, 10)}}).Plan(context.Background(), planning.PlanInput{Brief: b})
	require.NoError(t, err)
	assert.Equal(t, 10.0, plan.Scenes[0].Shots[0].DurationSeconds)
}

func TestDirector_RejectsUnusablePlans(t *testing.T) {
	for name, answer := range map[string]string{
		"no scene":      `{"scenes":[]}`,
		"no shot":       `{"scenes":[{"title":"S","shots":[]}]}`,
		"no title":      `{"scenes":[{"title":"","shots":[{"objective":"o","duration_seconds":3}]}]}`,
		"no objective":  `{"scenes":[{"title":"S","shots":[{"objective":"","duration_seconds":3}]}]}`,
		"zero duration": `{"scenes":[{"title":"S","shots":[{"objective":"o","duration_seconds":0}]}]}`,
	} {
		_, err := planning.NewDirector(&fakeLLM{answers: []string{answer}}).Plan(context.Background(), planning.PlanInput{Brief: brief()})
		assert.ErrorIs(t, err, planning.ErrInvalidPlan, name)
	}
}

func twoSceneScenePlan() domain.ScenePlan {
	return domain.ScenePlan{Scenes: []domain.PlannedScene{
		{Title: "A", Shots: []domain.PlannedShot{{Objective: "a1", DurationSeconds: 3}, {Objective: "a2", DurationSeconds: 3}}},
		{Title: "B", Shots: []domain.PlannedShot{{Objective: "b1", DurationSeconds: 3}}},
	}}
}

func TestStoryboarder_DrawsOneFramePerShot(t *testing.T) {
	answer := `{"scenes":[{"shots":[{"description":"a1 frame","framing":"CU"},{"description":"a2 frame"}]},{"shots":[{"description":"b1 frame","dialogue":"どうぞ"}]}]}`
	f := &fakeLLM{answers: []string{answer}}
	board, err := planning.NewStoryboarder(f).Draw(context.Background(), planning.BoardInput{Brief: brief(), Plan: twoSceneScenePlan()})
	require.NoError(t, err)
	require.Len(t, board.Shots, 2)
	require.Len(t, board.Shots[0], 2)
	assert.Equal(t, "CU", board.Shots[0][0].Framing)
	assert.Equal(t, "どうぞ", board.Shots[1][0].Dialogue)
	assert.Equal(t, []llm.Agent{llm.AgentStoryboard}, f.agents)
	assert.Contains(t, f.users[0], "a2", "the plan is the input of the Storyboard Agent")
}

func TestStoryboarder_RejectsFramesThatDoNotMatchThePlan(t *testing.T) {
	for name, answer := range map[string]string{
		"missing scene":  `{"scenes":[{"shots":[{"description":"x"},{"description":"y"}]}]}`,
		"missing frame":  `{"scenes":[{"shots":[{"description":"x"}]},{"shots":[{"description":"y"}]}]}`,
		"extra frame":    `{"scenes":[{"shots":[{"description":"x"},{"description":"y"}]},{"shots":[{"description":"y"},{"description":"z"}]}]}`,
		"empty describe": `{"scenes":[{"shots":[{"description":"x"},{"description":" "}]},{"shots":[{"description":"y"}]}]}`,
	} {
		_, err := planning.NewStoryboarder(&fakeLLM{answers: []string{answer}}).Draw(context.Background(), planning.BoardInput{Plan: twoSceneScenePlan()})
		assert.ErrorIs(t, err, planning.ErrInvalidPlan, name)
	}
}

func TestParseSeconds(t *testing.T) {
	for in, want := range map[string]float64{
		"15秒": 15, "30 sec": 30, "6s": 6, "1分": 60, "1分30秒": 90, "2min": 120, "  45 秒 ": 45, "7.5秒": 7.5,
	} {
		got, ok := planning.ParseSeconds(in)
		assert.True(t, ok, in)
		assert.InDelta(t, want, got, 0.001, in)
	}
	for _, in := range []string{"", "AIに任せる", "短め", "0秒"} {
		_, ok := planning.ParseSeconds(in)
		assert.False(t, ok, in)
	}
}
