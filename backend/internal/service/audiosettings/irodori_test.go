package audiosettings_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// voiceFile is a reference recording (a minimal wav) of the speaker.
func voiceFile(t *testing.T, name string, content []byte) localvoice.Voice {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, content, 0o600))
	return localvoice.Voice{Path: p}
}

var riff = append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 24)...)

func TestSave_IrodoriIsChosenLikeTheOtherLocalVoice(t *testing.T) {
	r := newRig(t, nil)

	require.NoError(t, r.svc.Save(context.Background(), audiosettings.Update{VoiceProvider: new(localvoice.Irodori)}))

	got, err := r.svc.Load()
	require.NoError(t, err)
	assert.Equal(t, localvoice.Irodori, got.VoiceProvider)
	assert.True(t, got.VoiceProvider.IsLocal())
	assert.False(t, localvoice.Local == localvoice.Irodori, "GPT-SoVITS keeps its own value, so a saved file of an earlier version still means GPT-SoVITS")
}

func TestView_ShowsIrodoriAndTheModelInForce(t *testing.T) {
	r := newRig(t, nil)
	v := r.rt.views["irodori-tts"]
	v.Choice = &choice.Choice{ID: "v4-large", Name: "Irodori-TTS v4-Large", Cautions: []string{"本人の同意なく声をコピーしない"}}
	r.rt.views["irodori-tts"] = v
	require.NoError(t, r.svc.Save(context.Background(), audiosettings.Update{VoiceProvider: new(localvoice.Irodori)}))

	view, err := r.svc.View(context.Background())

	require.NoError(t, err)
	require.NotNil(t, view.Irodori)
	assert.Equal(t, localvoice.Irodori, view.Settings.VoiceProvider)
	assert.Equal(t, "Irodori-TTS v4-Large", view.Irodori.Model)
	assert.Equal(t, []string{"本人の同意なく声をコピーしない"}, view.Irodori.Cautions)
	assert.True(t, view.Irodori.Usable)
}

func TestView_AnIrodoriThatNeedsTheLicenseToBeAcceptedSaysSo(t *testing.T) {
	r := newRig(t, nil)
	r.rt.refuse["irodori-tts"] = choice.ErrConsentRequired

	view, err := r.svc.View(context.Background())

	require.NoError(t, err)
	assert.False(t, view.Irodori.Usable)
	assert.Contains(t, view.Irodori.Reason, "ライセンス")
}

func TestPreflight_IrodoriThatCannotBeUsedGoesToTheCloudForTheWholeRunOnlyWithTheFallback(t *testing.T) {
	voices := map[string]localvoice.Voice{"ユイ": voiceFile(t, "yui.wav", riff)}
	r := newRig(t, fakeKeys{"elevenlabs": true})
	r.rt.refuse["irodori-tts"] = localruntime.ErrNotInstalled

	off, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Irodori, Voices: voices})
	require.NoError(t, err)
	on, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Irodori, Fallback: true, Voices: voices})
	require.NoError(t, err)

	assert.Equal(t, localvoice.Irodori, off.Voice, "without the fallback the run is made as asked and fails with its reason")
	assert.Equal(t, localvoice.ElevenLabs, on.Voice)
	require.Len(t, on.Notes, 1)
	assert.Contains(t, on.Notes[0], "Irodori-TTS")
	assert.Equal(t, []string{"irodori-tts"}, r.rt.checks, "only Irodori's own runtime was asked, not GPT-SoVITS")
}

func TestPreflight_IrodoriWithUsableRuntimeAndReferencesStaysLocal(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true})

	plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Irodori, Fallback: true, Voices: map[string]localvoice.Voice{"ユイ": voiceFile(t, "yui.wav", riff)}})

	require.NoError(t, err)
	assert.Equal(t, localvoice.Irodori, plan.Voice)
	assert.Empty(t, plan.Notes)
}

func TestPreflight_AReferenceIrodoriCannotBeGivenGoesToTheCloudWithTheFallback(t *testing.T) {
	voices := map[string]localvoice.Voice{"ユイ": voiceFile(t, "empty.wav", nil)}
	r := newRig(t, fakeKeys{"elevenlabs": true})

	plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Irodori, Fallback: true, Voices: voices})

	require.NoError(t, err)
	assert.Equal(t, localvoice.ElevenLabs, plan.Voice)
	require.Len(t, plan.Notes, 1)
	assert.Contains(t, plan.Notes[0], "空です")
	assert.Empty(t, r.rt.checks, "a recording that cannot be used is found before the runtime is asked")
}

func TestPreflight_ASpeakerWithoutAReferenceVoiceSendsIrodoriToTheCloudWithTheFallback(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true})

	plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Irodori, Fallback: true, Voices: map[string]localvoice.Voice{"ユイ": {}}})

	require.NoError(t, err)
	assert.Equal(t, localvoice.ElevenLabs, plan.Voice)
	require.Len(t, plan.Notes, 1)
	assert.Contains(t, plan.Notes[0], "ユイ")
}

func TestPreflight_TheCloudKeyIsNeededForIrodoriToo(t *testing.T) {
	r := newRig(t, nil)
	r.rt.refuse["irodori-tts"] = localruntime.ErrNotInstalled

	_, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Irodori, Fallback: true, Voices: map[string]localvoice.Voice{"ユイ": voiceFile(t, "yui.wav", riff)}})

	var nk *localvoice.NoCloudKeyError
	require.ErrorAs(t, err, &nk)
	assert.Equal(t, localvoice.RoleVoiceIrodori, nk.Role)
}
