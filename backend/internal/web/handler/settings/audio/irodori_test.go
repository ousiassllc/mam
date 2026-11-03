package audio_test

import (
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
)

func TestAudioPage_IrodoriIsASecondLocalChoiceOfTheVoiceWithTheCloneNoticeAndNoOtherCard(t *testing.T) {
	s := newScreen(t, machine(true), nil)

	html := s.page()

	assert.Contains(t, radio(t, html, "audio-voice_provider-irodori"), `value="irodori"`)
	assert.Contains(t, radio(t, html, "audio-voice_provider-local"), `value="local"`, "GPT-SoVITS keeps its own value")
	assert.Contains(t, radio(t, html, "audio-voice_provider-cloud"), "checked", "the default is still the cloud")
	assert.NotContains(t, html, `data-testid="audio-lip_sync_provider-irodori"`, "the lip sync has no Irodori")
	assert.Contains(t, html, `data-testid="audio-clone-notice-voice_provider"`)
	assert.Contains(t, html, "本人の同意を得た音声だけ", "the owner of a copied voice has to consent")
	assert.Contains(t, html, "Irodori-TTS（この PC。日本語専用）")
}

func TestAudioDetail_IrodoriHasItsOwnModalWithTheModelAndTheCautions(t *testing.T) {
	rt := machine(true)
	v := rt.views["irodori-tts"]
	v.Choice = &choice.Choice{ID: "v4-large", Name: "Irodori-TTS v4-Large", Cautions: []string{"本人の同意なく声をコピーしない"}}
	rt.views["irodori-tts"] = v
	s := newScreen(t, rt, nil)

	html := s.detail("voice", "irodori")

	assert.Contains(t, html, "Irodori-TTS", "the card of Irodori, not of GPT-SoVITS")
	assert.Contains(t, html, `data-testid="audio-model-voice_provider"`)
	assert.Contains(t, html, "Irodori-TTS v4-Large")
	assert.Contains(t, html, "モデルマネージャー")
	assert.Contains(t, html, "SilentCipher", "the generated audio carries an invisible watermark")
	assert.Contains(t, html, `data-testid="audio-cautions-voice_provider"`)
	assert.Contains(t, html, "本人の同意なく声をコピーしない")
	assert.NotContains(t, s.detail("voice", "local"), "audio-cautions-voice_provider", "GPT-SoVITS has no models to choose")
}

func TestAudioSave_IrodoriIsSavedAndShownAsTheChoice(t *testing.T) {
	s := newScreen(t, machine(true), keys{"elevenlabs": true})

	rec := s.save(url.Values{"voice_provider": {"irodori"}, "lip_sync_provider": {"synclabs"}, "fallback_to_cloud": {"1"}}, true)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, radio(t, rec.Body.String(), "audio-voice_provider-irodori"), "checked")
	assert.NotContains(t, radio(t, rec.Body.String(), "audio-voice_provider-local"), "checked")
	assert.NotContains(t, radio(t, rec.Body.String(), "audio-voice_provider-cloud"), "checked")
	raw, err := os.ReadFile(s.path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"voice_provider": "irodori"`)
	assert.Contains(t, radio(t, s.page(), "audio-voice_provider-irodori"), "checked", "a later load shows it")
}

func TestAudioSave_IrodoriThisPCCannotRunIsRefusedWithItsReasonAndNothingIsWritten(t *testing.T) {
	s := newScreen(t, machine(false), nil)

	rec := s.save(url.Values{"voice_provider": {"irodori"}, "lip_sync_provider": {"synclabs"}}, true)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "この PC ではローカルの声（Irodori-TTS）を選べません")
	assert.NoFileExists(t, s.path)
	assert.Contains(t, radio(t, s.page(), "audio-voice_provider-irodori"), "disabled")
}
