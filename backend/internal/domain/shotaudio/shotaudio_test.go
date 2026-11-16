package shotaudio_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/shotaudio"
)

func TestWith_ARoundTripKeepsEveryOtherKey(t *testing.T) {
	frame := domain.ShotStoryboard{Description: "a frame"}
	raw := domain.EncodeShotSpec(&frame)

	tied, err := shotaudio.With(raw, "asset-1")

	require.NoError(t, err)
	assert.Equal(t, "asset-1", shotaudio.ID(tied))
	got, ok := domain.Shot{ShotSpec: tied}.Storyboard()
	require.True(t, ok, "the storyboard stays")
	assert.Equal(t, frame, got)

	cleared, err := shotaudio.With(tied, "")

	require.NoError(t, err)
	assert.Empty(t, shotaudio.ID(cleared))
	assert.JSONEq(t, string(raw), string(cleared), "clearing gives the spec back as it was")
}

// The Canonical Shot Spec and the Reference Pack are written by merging into the current JSON: the audio is not theirs, so it survives both.
func TestWith_TheSpecAndThePackKeepTheAudio(t *testing.T) {
	tied, err := shotaudio.With(json.RawMessage(`{}`), "asset-1")
	require.NoError(t, err)

	merged, err := domain.MergeShotSpec(tied, domain.ShotSpec{ShotObjective: "run", Camera: domain.SpecCamera{Framing: "wide"}})
	require.NoError(t, err)
	packed, err := domain.WithReferences(merged, []domain.Reference{{Kind: domain.RefKeyframe, AssetID: "k"}})
	require.NoError(t, err)

	assert.Equal(t, "asset-1", shotaudio.ID(packed))
	spec, ok := domain.Shot{ShotSpec: packed}.CanonicalSpec()
	require.True(t, ok)
	assert.Equal(t, "run", spec.ShotObjective)
	assert.Len(t, domain.Shot{ShotSpec: packed}.References(), 1)
}

// A row written before the key existed (or an unreadable one) is a Shot without audio, never an error.
func TestID_ARowWithoutTheKeyHasNoAudio(t *testing.T) {
	for _, raw := range []string{``, `{}`, `null`, `[]`, `not json`, `{"audio":null}`, `{"audio":{}}`, `{"audio":"x"}`, `{"storyboard":{"description":"d"}}`} {
		assert.Empty(t, shotaudio.ID(json.RawMessage(raw)), "%q", raw)
	}
}

func TestWith_AnEmptyOrNullSpecBecomesAnObjectAndAForeignOneIsRefused(t *testing.T) {
	for _, raw := range []string{``, `null`, `{}`} {
		tied, err := shotaudio.With(json.RawMessage(raw), "a")
		require.NoError(t, err, "%q", raw)
		assert.JSONEq(t, `{"audio":{"asset_id":"a"}}`, string(tied))
	}
	_, err := shotaudio.With(json.RawMessage(`[1]`), "a")
	require.Error(t, err, "nothing is overwritten")
}
