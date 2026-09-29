package domain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestWorldContext_CurrentReadsEntityAndSingletonFields(t *testing.T) {
	w := domain.WorldContext{
		Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio", Wardrobe: "black_coat"}},
		Camera:     domain.WorldCamera{Lens: "35mm"},
		Story:      domain.WorldStory{Scene: 2, KnownInformation: []string{"heard_noise"}},
	}
	v, ok := w.Current(domain.TargetCharacter, "c1", "wardrobe")
	assert.True(t, ok)
	assert.JSONEq(t, `"black_coat"`, string(v))
	v, ok = w.Current(domain.TargetCharacter, "c1", "emotion")
	assert.True(t, ok, "the entity exists")
	assert.Nil(t, v, "an empty field has no value")
	_, ok = w.Current(domain.TargetCharacter, "nope", "wardrobe")
	assert.False(t, ok, "an unknown entity")
	v, _ = w.Current(domain.TargetCamera, "", "lens")
	assert.JSONEq(t, `"35mm"`, string(v))
	v, _ = w.Current(domain.TargetStory, "", "known_information")
	assert.JSONEq(t, `["heard_noise"]`, string(v))
	v, _ = w.Current(domain.TargetStory, "", "scene")
	assert.JSONEq(t, `2`, string(v))
}

func TestWorldContext_SetSingletonWritesAndClearsFields(t *testing.T) {
	var w domain.WorldContext
	require.NoError(t, w.SetSingleton(domain.TargetCamera, "movement", json.RawMessage(`"static"`)))
	require.NoError(t, w.SetSingleton(domain.TargetStory, "scene", json.RawMessage(`3`)))
	require.NoError(t, w.SetSingleton(domain.TargetStory, "known_information", json.RawMessage(`["a","b"]`)))
	assert.Equal(t, "static", w.Camera.Movement)
	assert.Equal(t, 3, w.Story.Scene)
	assert.Equal(t, []string{"a", "b"}, w.Story.KnownInformation)

	require.NoError(t, w.SetSingleton(domain.TargetCamera, "movement", json.RawMessage(`""`)))
	assert.Empty(t, w.Camera.Movement, "an empty value clears the field")
	assert.Error(t, w.SetSingleton(domain.TargetCharacter, "emotion", json.RawMessage(`"x"`)))
}

func TestSameValue_TreatsEmptyFormsAsEqual(t *testing.T) {
	assert.True(t, domain.SameValue(nil, json.RawMessage(`""`)))
	assert.True(t, domain.SameValue(json.RawMessage(`null`), json.RawMessage(`[]`)))
	assert.True(t, domain.SameValue(json.RawMessage(`["a"]`), json.RawMessage(`["a"]`)))
	assert.False(t, domain.SameValue(json.RawMessage(`["a"]`), json.RawMessage(`["a","b"]`)))
	assert.False(t, domain.SameValue(json.RawMessage(`"a"`), json.RawMessage(`"b"`)))
	assert.False(t, domain.SameValue(nil, json.RawMessage(`"a"`)))
}

func TestValidateFieldValue_ChecksShapeAndLength(t *testing.T) {
	long, _ := json.Marshal(strings.Repeat("あ", domain.MaxWorldValueRunes+1))
	ok200, _ := json.Marshal(strings.Repeat("あ", domain.MaxWorldValueRunes))
	assert.NoError(t, domain.ValidateFieldValue(domain.TargetCharacter, "wardrobe", ok200))
	assert.Error(t, domain.ValidateFieldValue(domain.TargetCharacter, "wardrobe", long))
	assert.Error(t, domain.ValidateFieldValue(domain.TargetCharacter, "wardrobe", json.RawMessage(`["x"]`)))
	assert.NoError(t, domain.ValidateFieldValue(domain.TargetStory, "known_information", json.RawMessage(`["a"]`)))
	assert.Error(t, domain.ValidateFieldValue(domain.TargetStory, "known_information", json.RawMessage(`"a"`)))
	assert.Error(t, domain.ValidateFieldValue(domain.TargetStory, "known_information", json.RawMessage(`[""]`)))
	assert.NoError(t, domain.ValidateFieldValue(domain.TargetStory, "scene", json.RawMessage(`1`)))
	assert.Error(t, domain.ValidateFieldValue(domain.TargetStory, "scene", json.RawMessage(`0`)))
}

func TestWorldContext_TextListsEveryPartTheAgentsNeed(t *testing.T) {
	assert.Empty(t, domain.WorldContext{}.Text(), "an empty world has no prompt text")
	text := domain.WorldContext{
		Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio", Emotion: "anxious", Position: "hallway_door"}},
		Camera:     domain.WorldCamera{Lens: "35mm"},
		Story:      domain.WorldStory{Scene: 8, PreviousEvent: "phone_call_ended"},
	}.Text()
	assert.Contains(t, text, `id="c1"`)
	assert.Contains(t, text, `emotion="anxious"`)
	assert.Contains(t, text, `position="hallway_door"`)
	assert.Contains(t, text, `lens="35mm"`)
	assert.Contains(t, text, `previous_event="phone_call_ended"`)
}
