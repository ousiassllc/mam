package localspeech_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

var yuiLines = []audiodirector.Scene{{Lines: []string{"ユイ: こんにちは", "ハル: やあ"}}}

// A Character that was trained speaks with its trained voice; one that was not keeps the zero-shot clone: the choice is per speaker.
func TestVoices_ACharacterWithATrainedVoiceCarriesItsNameAndTheOthersDoNot(t *testing.T) {
	l := &lookup{voices: map[string]voicerefsvc.Voice{
		"ユイ": {Path: "/a/yui.wav", Reference: voiceref.Reference{Language: "ja"}, Trained: &voicerefsvc.Trained{ID: "v-1"}},
		"ハル": {Path: "/a/haru.wav", Reference: voiceref.Reference{Language: "ja"}},
	}}

	got, err := localspeech.Voices(context.Background(), l, "p1", "", yuiLines)

	require.NoError(t, err)
	assert.Equal(t, "v-1", got["ユイ"].Trained)
	assert.Empty(t, got["ハル"].Trained)
	assert.True(t, got["ユイ"].Usable())
}

// Weights that are gone or damaged do not fall back to the zero-shot clone: the speaker is refused with the cause, and the sentence says what to do.
func TestVoices_ABrokenTrainedVoiceIsAnUnusableSpeakerAndTheSentenceTellsTheWayOut(t *testing.T) {
	l := &lookup{
		voices: map[string]voicerefsvc.Voice{"ハル": {Path: "/a/haru.wav", Reference: voiceref.Reference{Language: "ja"}}},
		errs:   map[string]error{"ユイ": fmt.Errorf("%w: %q", voicerefsvc.ErrNoWeights, "ユイ")},
	}

	got, err := localspeech.Voices(context.Background(), l, "p1", "", yuiLines)

	require.NoError(t, err)
	assert.False(t, got["ユイ"].Usable())
	assert.ErrorIs(t, got["ユイ"].Cause, localvoice.ErrTrainedVoice)
	assert.ErrorIs(t, got["ユイ"].Cause, voicerefsvc.ErrNoWeights)
	assert.Empty(t, got["ユイ"].Path, "no recording is offered to speak with instead")

	lines := []domain.VoiceLine{{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "こんにちは"}, {Kind: domain.VoiceDialogue, Speaker: "ハル", Text: "やあ"}}
	err = localvoice.Check(lines, got, true, func() error { return nil })
	text, ok := localspeech.Reason(err)
	require.True(t, ok)
	assert.Contains(t, text, "学習した声のファイルが無いか壊れている話者がいます: 「ユイ」")
	assert.Contains(t, text, "「学習した声」を削除してゼロショットの声に戻すか、学習し直してください")
	assert.NotContains(t, text, "参照音声が無い話者がいます", "the speaker did not lack a reference voice")
}

func TestReason_ABrokenTrainedVoiceAndAMissingReferenceAreToldApart(t *testing.T) {
	err := &localvoice.MissingVoiceError{Speakers: []localvoice.MissingSpeaker{
		{Name: "ユイ", Cause: fmt.Errorf("%w: %w", localvoice.ErrTrainedVoice, voicerefsvc.ErrNoWeights)},
		{Name: "ハル", Cause: voicerefsvc.ErrNoReference},
	}}

	text, ok := localspeech.Reason(err)

	require.True(t, ok)
	assert.Contains(t, text, "学習した声のファイルが無いか壊れている話者がいます: 「ユイ」")
	assert.Contains(t, text, "参照音声が無い話者もいます: 「ハル」")
}
