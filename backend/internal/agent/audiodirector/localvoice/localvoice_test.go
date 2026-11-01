package localvoice_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

func TestProvider_TheZeroValueIsElevenLabs(t *testing.T) {
	assert.True(t, localvoice.Provider("").Valid())
	assert.True(t, localvoice.ElevenLabs.Valid())
	assert.True(t, localvoice.Local.Valid())
	assert.False(t, localvoice.Provider("azure").Valid())
	assert.False(t, localvoice.Provider("ElevenLabs").Valid(), "the identifiers are the lower-case ones")
}

func TestSpeakerOf_TheNarrationIsTheCharacterNamedNarration(t *testing.T) {
	assert.Equal(t, "ユイ", localvoice.SpeakerOf(domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}))
	assert.Equal(t, voiceref.NarrationName, localvoice.SpeakerOf(domain.VoiceLine{Kind: domain.VoiceNarration, Text: "a"}))
}

func TestVoice_UsableNeedsAPathAndNoCause(t *testing.T) {
	assert.True(t, localvoice.Voice{Path: "/a.wav"}.Usable())
	assert.False(t, localvoice.Voice{}.Usable())
	assert.False(t, localvoice.Voice{Path: "/a.wav", Cause: errors.New("x")}.Usable())
}

func TestCheck(t *testing.T) {
	lines := []domain.VoiceLine{
		{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"},
		{Kind: domain.VoiceNarration, Text: "b"},
		{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "c"},
	}
	all := map[string]localvoice.Voice{"ユイ": {Path: "/y.wav"}, voiceref.NarrationName: {Path: "/n.wav"}}
	started := 0
	start := func() error { started++; return nil }

	t.Run("starts the server once all the voices are there", func(t *testing.T) {
		started = 0
		require.NoError(t, localvoice.Check(lines, all, true, start))
		assert.Equal(t, 1, started)
	})
	t.Run("a missing voice stops it before the server is started, naming each speaker once", func(t *testing.T) {
		started = 0
		err := localvoice.Check(lines, map[string]localvoice.Voice{}, true, start)

		var mv *localvoice.MissingVoiceError
		require.ErrorAs(t, err, &mv)
		assert.Equal(t, []string{"ユイ", voiceref.NarrationName}, mv.Names())
		assert.Zero(t, started)
	})
	t.Run("the cause of each speaker is behind the error", func(t *testing.T) {
		cause := errors.New("no reference")
		err := localvoice.Check(lines, map[string]localvoice.Voice{"ユイ": {Cause: cause}, voiceref.NarrationName: all[voiceref.NarrationName]}, true, start)

		assert.ErrorIs(t, err, cause)
		assert.ErrorContains(t, err, "ユイ")
		assert.NotContains(t, err.Error(), voiceref.NarrationName)
	})
	t.Run("a provider that is not configured is kind not_installed", func(t *testing.T) {
		err := localvoice.Check(lines, all, false, start)

		var un *localvoice.UnavailableError
		require.ErrorAs(t, err, &un)
		assert.Equal(t, localvoice.KindNotInstalled, un.Kind)
	})
	t.Run("the error of the start is returned as it is", func(t *testing.T) {
		want := &localvoice.UnavailableError{Kind: localvoice.KindGPUMemory}

		assert.Same(t, want, localvoice.Check(lines, all, true, func() error { return want }))
	})
	t.Run("no lines, nothing to check", func(t *testing.T) {
		started = 0
		require.NoError(t, localvoice.Check(nil, nil, false, start))
		assert.Zero(t, started)
	})
}

func TestUnavailableError_MessageAndUnwrap(t *testing.T) {
	inner := errors.New("inner")
	e := &localvoice.UnavailableError{Kind: localvoice.KindProcess, Detail: "exited", Err: inner}

	assert.ErrorContains(t, e, "process")
	assert.ErrorContains(t, e, "exited")
	assert.ErrorIs(t, e, inner)
	assert.NotContains(t, (&localvoice.UnavailableError{Kind: localvoice.KindTimeout}).Error(), "): ")
}
