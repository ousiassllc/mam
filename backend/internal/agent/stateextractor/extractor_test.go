package stateextractor_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
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

func locationInput() stateextractor.Input {
	in := input()
	in.World.Locations = []domain.WorldLocation{
		{ID: "l1", Name: "Street", Time: "sunset", LightingState: "warm", Weather: "clear"},
		{ID: "l2", Name: "Cafe", Time: "noon"},
	}
	in.Spec.Location = &domain.SpecLocation{ID: "Street"}
	return in
}

func TestExtract_UpdatesTheLocationStateOfTheShotsLocationOnly(t *testing.T) {
	diff, f := extract(t, `{"changes":[
		{"target":"location","id":"l1","field":"time","value":"night"},
		{"target":"location","id":"l1","field":"weather","value":"clear"},
		{"target":"location","id":"l1","field":"layout_reference","value":"x"},
		{"target":"location","id":"l2","field":"time","value":"evening"}]}`, locationInput())

	assert.Equal(t, []string{"location.l1.time"}, fields(diff), "same weather, non-extractable layout_reference and another Location are left out")
	assert.JSONEq(t, `"sunset"`, string(diff.Changes[0].From))
	assert.Contains(t, f.system, "lighting_state")
	assert.Contains(t, f.user, "sunset", "the model sees the current Location State")
}

func TestExtract_LockedLocationFieldsAreNotChanged(t *testing.T) {
	in := locationInput()
	in.Locks = domain.LockSet{{Target: domain.TargetLocation, ID: "l1", Field: "lighting_state"}: {}}
	diff, _ := extract(t, `{"changes":[
		{"target":"location","id":"l1","field":"lighting_state","value":"dark"},
		{"target":"location","id":"l1","field":"weather","value":"rain"}]}`, in)
	assert.Equal(t, []string{"location.l1.weather"}, fields(diff))
}

func TestExtract_NoLocationChangeWhenTheShotHasNoKnownLocation(t *testing.T) {
	in := locationInput()
	in.Spec.Location = nil
	diff, _ := extract(t, `{"changes":[{"target":"location","id":"l1","field":"time","value":"night"}]}`, in)
	assert.Empty(t, diff.Changes)
}

func TestExtract_PromptTellsTheModelTheKnownInformationLimit(t *testing.T) {
	_, f := extract(t, `{"changes":[]}`, input())
	assert.Contains(t, f.system, "at most "+strconv.Itoa(domain.MaxKnownInformation)+" items")
	assert.Contains(t, f.system, "drop the oldest or least important items")
}

func TestExtract_LogsAWarningWhenAnInvalidChangeIsLeftOut(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	items := make([]string, domain.MaxKnownInformation+1)
	for i := range items {
		items[i] = fmt.Sprintf("fact_%d", i)
	}
	over := `["` + strings.Join(items, `","`) + `"]`
	f := &fakeLLM{answer: `{"changes":[{"target":"story","field":"known_information","value":` + over + `}]}`}

	diff, err := stateextractor.New(f, stateextractor.WithLogger(logger)).Extract(context.Background(), input())
	require.NoError(t, err)
	assert.Empty(t, diff.Changes, "a list over the limit is not applied")
	assert.Contains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), "known_information")
	assert.Contains(t, buf.String(), "more than 20 items")

	buf.Reset()
	f.answer = `{"changes":[{"target":"character","id":"zzz","field":"emotion","value":"x"}]}`
	_, err = stateextractor.New(f, stateextractor.WithLogger(logger)).Extract(context.Background(), input())
	require.NoError(t, err)
	assert.Empty(t, buf.String(), "other drops stay quiet")
}
