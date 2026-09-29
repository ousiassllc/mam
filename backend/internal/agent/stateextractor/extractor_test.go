package stateextractor_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type fakeLLM struct {
	answer string
	agent  llm.Agent
	system string
	user   string
}

func (f *fakeLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.agent, f.system, f.user = agent, req.System, req.User
	return llm.Response{Text: f.answer, Provider: llm.ProviderGoogle, Model: "fake"}, nil
}

func input() stateextractor.Input {
	return stateextractor.Input{
		Scene: domain.Scene{OrderIndex: 7, Title: "Hallway"},
		Shot:  domain.Shot{OrderIndex: 0, Objective: "She reaches for the handle", Duration: 4},
		Spec:  domain.ShotSpec{ShotObjective: "She reaches for the handle"},
		World: domain.WorldContext{
			Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio", Wardrobe: "black_coat", Emotion: "calm"}},
			Products:   []domain.WorldProduct{{ID: "p1", Name: "Serum", Condition: "unopened"}},
			Camera:     domain.WorldCamera{Movement: "static"},
			Story:      domain.WorldStory{Scene: 8, KnownInformation: []string{"heard_noise"}},
		},
		Locks: domain.LockSet{},
	}
}

func extract(t *testing.T, answer string, in stateextractor.Input) (domain.StateDiff, *fakeLLM) {
	t.Helper()
	f := &fakeLLM{answer: answer}
	diff, err := stateextractor.New(f).Extract(context.Background(), in)
	require.NoError(t, err)
	return diff, f
}

func fields(d domain.StateDiff) []string {
	var out []string
	for _, c := range d.Changes {
		out = append(out, string(c.Target)+"."+c.ID+"."+c.Field)
	}
	return out
}

func TestExtract_ReturnsOnlyWhatChangedWithItsPreviousValue(t *testing.T) {
	diff, f := extract(t, `{"changes":[
		{"target":"character","id":"c1","field":"emotion","value":"anxious"},
		{"target":"character","id":"c1","field":"wardrobe","value":"black_coat"},
		{"target":"camera","field":"movement","value":"slow_push_in"},
		{"target":"story","field":"known_information","value":["heard_noise","found_key"]}]}`, input())

	assert.Equal(t, llm.AgentStateExtractor, f.agent)
	assert.Equal(t, []string{"character.c1.emotion", "camera..movement", "story..known_information"}, fields(diff),
		"an unchanged wardrobe is not a change; scene index 7 is scene 8, which the story is already at")
	assert.JSONEq(t, `"calm"`, string(diff.Changes[0].From))
	assert.JSONEq(t, `"anxious"`, string(diff.Changes[0].To))
	assert.Contains(t, f.user, `emotion="calm"`, "the model compares against the current world state")
	assert.Contains(t, f.user, "She reaches for the handle")
}

func TestExtract_LeavesLockedFieldsOutOfTheDiffAndTellsTheModelNothingAboutThem(t *testing.T) {
	in := input()
	in.Locks = domain.LockSet{
		{Target: domain.TargetCharacter, ID: "c1", Field: "emotion"}: {},
		{Target: domain.TargetCamera, Field: "movement"}:             {},
		{Target: domain.TargetStory, Field: "scene"}:                 {},
	}
	diff, _ := extract(t, `{"changes":[
		{"target":"character","id":"c1","field":"emotion","value":"anxious"},
		{"target":"camera","field":"movement","value":"slow_push_in"},
		{"target":"camera","field":"lens","value":"85mm"},
		{"target":"product","id":"p1","field":"condition","value":"opened"}]}`, in)

	assert.Equal(t, []string{"camera..lens", "product.p1.condition"}, fields(diff), "LOCKed emotion, camera movement and story scene are never changed")
}

func TestExtract_DropsAnswersThatCannotBeApplied(t *testing.T) {
	in := input()
	in.Scene.OrderIndex = 2
	diff, _ := extract(t, `{"changes":[
		{"target":"character","id":"ghost","field":"emotion","value":"happy"},
		{"target":"character","id":"c1","field":"personality","value":"cheerful"},
		{"target":"character","id":"c1","field":"emotion","value":["a"]},
		{"target":"character","id":"c1","field":"position","value":""},
		{"target":"story","field":"scene","value":99},
		{"target":"location","id":"l1","field":"time","value":"night"},
		{"target":"character","id":"c1","field":"current_action","value":"opening_door"},
		{"target":"character","id":"c1","field":"current_action","value":"closing_door"}]}`, in)

	assert.Equal(t, []string{"story..scene", "character.c1.current_action"}, fields(diff),
		"unknown entity, non-extractable field, wrong shape, empty value, model-set scene and duplicates are dropped")
	assert.JSONEq(t, `"opening_door"`, string(diff.Changes[1].To), "the first answer for a field wins")
}

func TestExtract_StoryFollowsTheSceneOfTheShotEvenWhenTheModelSaysNothing(t *testing.T) {
	in := input()
	in.Scene.OrderIndex = 8
	diff, _ := extract(t, `{"changes":[]}`, in)
	require.Len(t, diff.Changes, 1)
	assert.Equal(t, domain.TargetStory, diff.Changes[0].Target)
	assert.JSONEq(t, `8`, string(diff.Changes[0].From))
	assert.JSONEq(t, `9`, string(diff.Changes[0].To))

	diff, _ = extract(t, `{"changes":[]}`, input())
	assert.NotNil(t, diff.Changes)
	assert.Empty(t, diff.Changes, "nothing changed at all")
}

func TestExtract_AnUnusableAnswerIsAnError(t *testing.T) {
	_, err := stateextractor.New(&fakeLLM{answer: "no json here"}).Extract(context.Background(), input())
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
}

func TestExtract_PromptOffersOnlyTheFieldsTheModelMayChange(t *testing.T) {
	_, f := extract(t, `{"changes":[]}`, input())
	assert.Contains(t, f.system, "emotion")
	assert.Contains(t, f.system, "known_information")
	assert.NotContains(t, f.system, "personality", "personality is set by the user only")
	assert.NotContains(t, f.system, "voice")
}
