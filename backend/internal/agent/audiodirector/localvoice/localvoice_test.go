package localvoice_test

import (
	"errors"
	"fmt"
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

func TestLipSyncProvider_TheZeroValueIsSyncLabs(t *testing.T) {
	assert.True(t, localvoice.LipSyncProvider("").Valid())
	assert.True(t, localvoice.LipSyncSyncLabs.Valid())
	assert.True(t, localvoice.LipSyncLocal.Valid())
	assert.False(t, localvoice.LipSyncProvider("sync").Valid())
	assert.False(t, localvoice.LipSyncProvider("Local").Valid(), "the identifiers are the lower-case ones")
}

func TestInvalidProvider_NamesTheOptionThatIsWrong(t *testing.T) {
	assert.Empty(t, localvoice.InvalidProvider("", ""))
	assert.Empty(t, localvoice.InvalidProvider(localvoice.Local, localvoice.LipSyncLocal))
	assert.Equal(t, "voice_provider", localvoice.InvalidProvider("azure", localvoice.LipSyncLocal))
	assert.Equal(t, "lip_sync_provider", localvoice.InvalidProvider(localvoice.Local, "azure"))
	assert.Equal(t, "voice_provider", localvoice.InvalidProvider("azure", "azure"), "the first is told first")
}

func TestUnavailableError_NamesItsProviderAndTellsAWholeRuntimeFromOneClip(t *testing.T) {
	voice := &localvoice.UnavailableError{Kind: localvoice.KindProcess, Detail: "d"}
	lip := &localvoice.UnavailableError{Kind: localvoice.KindProcess, Vendor: localvoice.VendorMuseTalk, Detail: "d"}
	assert.Contains(t, voice.Error(), "GPT-SoVITS")
	assert.Contains(t, lip.Error(), "MuseTalk")
	assert.Contains(t, lip.Error(), "process")

	systemic := map[localvoice.Kind]bool{
		localvoice.KindNotInstalled: true, localvoice.KindWeightsMissing: true, localvoice.KindUnsupported: true,
		localvoice.KindGPUMemory: false, localvoice.KindProcess: false, localvoice.KindTimeout: false, localvoice.KindNoFace: false, localvoice.KindTooLarge: false,
	}
	for kind, want := range systemic {
		assert.Equal(t, want, (&localvoice.UnavailableError{Kind: kind}).Systemic(), kind)
	}
}

func TestLipSyncReason_EveryKindHasItsOwnSentenceWithTheDetail(t *testing.T) {
	kinds := []localvoice.Kind{
		localvoice.KindNotInstalled, localvoice.KindWeightsMissing, localvoice.KindUnsupported, localvoice.KindGPUMemory,
		localvoice.KindProcess, localvoice.KindTimeout, localvoice.KindNoFace, localvoice.KindTooLarge,
	}
	seen := map[string]localvoice.Kind{}
	for _, kind := range kinds {
		text := localvoice.LipSyncReason(fmt.Errorf("clip 1: %w", &localvoice.UnavailableError{Kind: kind, Vendor: localvoice.VendorMuseTalk, Detail: "理由の詳細"}))
		assert.Contains(t, text, "理由の詳細", kind)
		assert.NotContains(t, text, "GPT-SoVITS", kind)
		other, dup := seen[text]
		assert.False(t, dup, "%s has the sentence of %s", kind, other)
		seen[text] = kind
	}
	assert.Contains(t, localvoice.LipSyncReason(&localvoice.UnavailableError{Kind: "new"}), "MuseTalk", "a kind that is not known yet is still the local lip sync")
	assert.Equal(t, "boom", localvoice.LipSyncReason(errors.New("boom")), "any other error is told as it is")
	assert.NotContains(t, localvoice.LipSyncReason(&localvoice.UnavailableError{Kind: localvoice.KindNoFace}), "（", "no detail, no parentheses")
}
