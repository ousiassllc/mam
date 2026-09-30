package promptplanner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/agent/promptplanner"
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

const good = `{"shot_objective":"Character notices something behind her and starts running","duration":"9.9s",
	"character":{"id":"Character_A","identity":"locked","wardrobe":"inherit_previous"},
	"acting":[{"time":"0.0-0.7","action":"subtle reaction"},{"time":"0.7-4.0","action":"begin running"}],
	"camera":{"lens":"35mm","framing":"medium"},"lighting":{"mood":"cold blue"}}`

func input() promptplanner.Input {
	return promptplanner.Input{
		Brief:       domain.Brief{Subject: "美容液", Duration: "15秒"},
		Template:    domain.TemplateDrama,
		Scene:       domain.Scene{Title: "路地", Location: "夜の路地"},
		Shot:        domain.Shot{Objective: "主人公が振り返って走り出す", Duration: 4},
		Instruction: "もう少しカメラを寄せて",
	}
}

func TestPlan_ConvertsTheInstructionToACanonicalSpecWithTheShotDuration(t *testing.T) {
	f := &fakeLLM{answer: good}
	spec, err := promptplanner.New(f).Plan(context.Background(), input())
	require.NoError(t, err)

	assert.Equal(t, llm.AgentPromptPlanner, f.agent)
	assert.Equal(t, "4.0s", spec.Duration, "the duration is the Shot's, whatever the model wrote")
	assert.Equal(t, "Character_A", spec.Character.ID)
	assert.Contains(t, f.user, "主人公が振り返って走り出す", "the Shot's objective reaches the model")
	assert.Contains(t, f.user, "もう少しカメラを寄せて", "so does the user's instruction")
}

func TestPlan_RejectsSpecsThatCannotDriveAGeneration(t *testing.T) {
	cases := map[string]string{
		"not English":      `{"shot_objective":"主人公が走る","camera":{"framing":"medium"}}`,
		"no framing":       `{"shot_objective":"A woman runs","camera":{}}`,
		"acting too long":  `{"shot_objective":"A woman runs","camera":{"framing":"wide"},"acting":[{"time":"0.0-9.0","action":"runs"}]}`,
		"acting reversed":  `{"shot_objective":"A woman runs","camera":{"framing":"wide"},"acting":[{"time":"2.0-3.0","action":"a"},{"time":"0.0-1.0","action":"b"}]}`,
		"no objective":     `{"shot_objective":" ","camera":{"framing":"wide"}}`,
		"not a JSON value": `sorry, I cannot`,
	}
	for name, answer := range cases {
		_, err := promptplanner.New(&fakeLLM{answer: answer}).Plan(context.Background(), input())
		require.Error(t, err, name)
		if name != "not a JSON value" {
			assert.ErrorIs(t, err, planning.ErrInvalidPlan, name)
		}
	}
}

func TestPlan_ExistingSpecAndPreviousShotAreGivenToTheModel(t *testing.T) {
	f := &fakeLLM{answer: good}
	in := input()
	prev := domain.ShotSpec{ShotObjective: "She opens the door", Character: &domain.SpecActor{ID: "Character_A", Wardrobe: "black_coat_v2"}}
	cur := domain.ShotSpec{ShotObjective: "She runs", Camera: domain.SpecCamera{Framing: "wide"}}
	in.Previous, in.Current = &prev, &cur
	_, err := promptplanner.New(f).Plan(context.Background(), in)
	require.NoError(t, err)
	assert.Contains(t, f.user, "black_coat_v2", "wardrobe continuity comes from the previous shot")
	assert.Contains(t, f.user, "She runs", "a revision starts from the current spec")
}

func TestPlan_WorldStateLeftByThePreviousShotIsGivenToTheModel(t *testing.T) {
	f := &fakeLLM{answer: good}
	in := input()
	in.World = domain.WorldContext{
		Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio", Emotion: "anxious", Position: "hallway_door", CurrentAction: "reaching_for_handle"}},
		Camera:     domain.WorldCamera{Lens: "35mm", Movement: "static"},
		Story:      domain.WorldStory{Scene: 8, PreviousEvent: "phone_call_ended"},
	}
	_, err := promptplanner.New(f).Plan(context.Background(), in)
	require.NoError(t, err)
	for _, want := range []string{"anxious", "hallway_door", "reaching_for_handle", `lens="35mm"`, "phone_call_ended"} {
		assert.Contains(t, f.user, want, "the next shot starts from the state the last one left")
	}
}

func TestPlan_EachCandidateStrategyPutsItsOwnPriorityToTheModel(t *testing.T) {
	want := map[domain.CandidateStrategy]string{
		domain.CandidateStabilityFirst: "rock steady",
		domain.CandidateActingFirst:    "the performance",
		domain.CandidateDynamicCamera:  "cinematic camera work",
	}
	require.Len(t, want, len(domain.CandidateStrategies()), "every strategy is covered")
	for _, strategy := range domain.CandidateStrategies() {
		f := &fakeLLM{answer: good}
		in := input()
		in.Strategy = strategy

		_, err := promptplanner.New(f).Plan(context.Background(), in)

		require.NoError(t, err)
		assert.Contains(t, f.user, want[strategy], strategy)
		assert.Contains(t, f.user, "主人公が振り返って走り出す", "the Shot itself is still planned")
	}

	f := &fakeLLM{answer: good}
	_, err := promptplanner.New(f).Plan(context.Background(), input())
	require.NoError(t, err)
	assert.NotContains(t, f.user, "Shooting strategy", "a plain plan has no strategy section")
}

func TestPlan_FixedPartsAreCopiedFromTheCurrentSpecWhateverTheModelAnswers(t *testing.T) {
	current := domain.ShotSpec{
		ShotObjective: "She runs", Duration: "4.0s",
		Character: &domain.SpecActor{ID: "Character_A", Wardrobe: "black_coat"},
		Acting:    []domain.ActingBeat{{Time: "0.0-4.0", Action: "run"}},
		Camera:    domain.SpecCamera{Framing: "wide", Movement: "static"},
	}
	in := input()
	in.Current = &current
	in.Fixed = []domain.SpecPart{domain.PartCharacter, domain.PartCamera}
	in.Manual = []domain.SpecPart{domain.PartActing}
	f := &fakeLLM{answer: good}

	spec, err := promptplanner.New(f).Plan(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, "black_coat", spec.Character.Wardrobe)
	assert.Equal(t, "static", spec.Camera.Movement, "the model's camera is replaced by the LOCKed one")
	assert.Equal(t, "wide", spec.Camera.Framing)
	assert.Equal(t, "begin running", spec.Acting[1].Action, "unfixed parts follow the model")
	assert.Contains(t, f.user, "Fixed parts")
	assert.Contains(t, f.user, "character, camera")
	assert.Contains(t, f.user, "Parts the user specified himself")
}

func TestPlan_FixedPartsAreIgnoredWithoutACurrentSpec(t *testing.T) {
	in := input()
	in.Fixed = []domain.SpecPart{domain.PartCamera}
	f := &fakeLLM{answer: good}

	spec, err := promptplanner.New(f).Plan(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, "medium", spec.Camera.Framing)
	assert.NotContains(t, f.user, "Fixed parts")
}

func TestPlan_NotesAreGivenToTheModelAsContext(t *testing.T) {
	f := &fakeLLM{answer: good}
	in := input()
	in.Notes = []domain.NoteView{{
		Note:    domain.Note{ID: "n1", Category: domain.NoteCategoryTone},
		Version: domain.NoteVersion{Number: 2, Title: "口調", Content: "主人公は落ち着いた口調で話す"},
	}}
	_, err := promptplanner.New(f).Plan(context.Background(), in)
	require.NoError(t, err)
	assert.Contains(t, f.user, "主人公は落ち着いた口調で話す")

	g := &fakeLLM{answer: good}
	_, err = promptplanner.New(g).Plan(context.Background(), input())
	require.NoError(t, err)
	assert.NotContains(t, g.user, "Production notes")
}
