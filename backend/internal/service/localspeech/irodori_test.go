package localspeech_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// stateful is a runtime manager whose process is stopped until Prepare starts it; it records the order of what happened.
type stateful struct {
	events  *[]string
	started bool
	err     error
}

func (r *stateful) Prepare(_ context.Context, id string) error {
	*r.events = append(*r.events, "prepare "+id)
	if r.err != nil {
		return r.err
	}
	r.started = true
	return nil
}

func (r *stateful) Status(context.Context, string) (localruntime.View, error) {
	state := proc.StateStopped
	if r.started {
		state = proc.StateRunning
	}
	return localruntime.View{Process: proc.Status{State: state, URL: "http://127.0.0.1:9990"}}, nil
}

func (r *stateful) Token(string) (string, error) { return "tok", nil }

func TestIrodoriServer_StartsItsOwnRuntimeAndTagsItsErrorsWithItsVendor(t *testing.T) {
	var events []string
	rt := &stateful{events: &events}

	base, token, err := localspeech.IrodoriServer(rt, nil).Ensure(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:9990", base)
	assert.Equal(t, "tok", token)
	assert.Equal(t, []string{"prepare irodori-tts"}, events, "Irodori's runtime, not GPT-SoVITS's")

	rt = &stateful{events: &events, err: localruntime.ErrNotInstalled}
	_, _, err = localspeech.IrodoriServer(rt, nil).Ensure(context.Background())
	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Equal(t, localvoice.VendorIrodori, un.Vendor)
	assert.Contains(t, un.Error(), "Irodori-TTS")
}

func TestIrodoriServer_TheGPUIsMadeRoomForBeforeTheServerStartsAndOnlyThen(t *testing.T) {
	var events []string
	rt := &stateful{events: &events}
	before := func(context.Context) error { events = append(events, "free the GPU"); return nil }
	server := localspeech.IrodoriServer(rt, before)

	_, _, err := server.Ensure(context.Background())
	require.NoError(t, err)
	_, _, err = server.Ensure(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []string{"free the GPU", "prepare irodori-tts", "prepare irodori-tts"}, events, "a server that already runs is not disturbed again")
}

func TestIrodoriServer_AGPUThatCannotBeMadeRoomForRefusesTheStartWithItsError(t *testing.T) {
	var events []string
	rt := &stateful{events: &events}
	busy := &localvoice.UnavailableError{Kind: localvoice.KindGPUMemory, Vendor: localvoice.VendorIrodori, Detail: "ComfyUI が生成中です"}

	_, _, err := localspeech.IrodoriServer(rt, func(context.Context) error { return busy }).Ensure(context.Background())

	assert.ErrorIs(t, err, busy)
	assert.Empty(t, events, "the runtime is not started")
}

func TestUnavailableFor_ALicenseThatWasNotAcceptedIsItsOwnKind(t *testing.T) {
	got := localspeech.UnavailableFor(context.Background(), localvoice.VendorIrodori, choice.ErrConsentRequired)

	var un *localvoice.UnavailableError
	require.ErrorAs(t, got, &un)
	assert.Equal(t, localvoice.KindConsent, un.Kind)
	assert.True(t, un.Systemic())
	assert.ErrorIs(t, got, choice.ErrConsentRequired)
}

func TestReason_AReferenceVoiceIrodoriRefusedNamesTheSpeakerAndWhatToDo(t *testing.T) {
	got, ok := localspeech.Reason(&localvoice.ReferenceError{Speaker: "ユイ", Problem: "参照音声のファイルが空です", Vendor: localvoice.VendorIrodori})

	require.True(t, ok)
	assert.Contains(t, got, "Irodori-TTS")
	assert.Contains(t, got, "ユイ")
	assert.Contains(t, got, "参照音声のファイルが空です")
	assert.Contains(t, got, "ElevenLabs")
}

func TestReason_SpeakersWithoutAVoiceNameTheVoiceThatWasAsked(t *testing.T) {
	mv := func(vendor string) error {
		return &localvoice.MissingVoiceError{Vendor: vendor, Speakers: []localvoice.MissingSpeaker{{Name: "ユイ", Cause: voicerefsvc.ErrNoReference}}}
	}

	irodori, _ := localspeech.Reason(mv(localvoice.VendorIrodori))
	gptsovits, _ := localspeech.Reason(mv(""))

	assert.Contains(t, irodori, "Irodori-TTS")
	assert.NotContains(t, irodori, "GPT-SoVITS")
	assert.Contains(t, gptsovits, "GPT-SoVITS")
}

func TestVoices_CarryTheLengthOfTheRecordingAndWhatTheCharacterSaysOfTheVoice(t *testing.T) {
	l := &lookup{voices: map[string]voicerefsvc.Voice{
		"ユイ": {
			Path:      "/a/yui.wav",
			Reference: voiceref.Reference{Text: "ユイです", Language: "ja", DurationMS: 7500},
			Character: domain.WorldCharacter{Voice: "落ち着いた声", Personality: "控えめ"},
		},
	}}

	got, err := localspeech.Voices(context.Background(), l, "p1", "", []audiodirector.Scene{{Lines: []string{"ユイ: こんにちは"}}})

	require.NoError(t, err)
	assert.Equal(t, 7500*time.Millisecond, got["ユイ"].Duration)
	assert.Equal(t, localvoice.Profile{Voice: "落ち着いた声", Personality: "控えめ"}, got["ユイ"].Profile)
}
