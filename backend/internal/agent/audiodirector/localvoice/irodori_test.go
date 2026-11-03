package localvoice_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestProvider_IrodoriIsASecondLocalVoiceAndGPTSoVITSKeepsItsOwnValue(t *testing.T) {
	assert.Equal(t, localvoice.Provider("local"), localvoice.Local, "a saved audio.json of an earlier version still means GPT-SoVITS")
	assert.Equal(t, localvoice.Provider("irodori"), localvoice.Irodori)
	for _, p := range []localvoice.Provider{localvoice.Local, localvoice.Irodori} {
		assert.True(t, p.IsLocal(), p)
		assert.True(t, p.Valid(), p)
	}
	assert.False(t, localvoice.ElevenLabs.IsLocal())
	assert.False(t, localvoice.Provider("azure").Valid())
	assert.Equal(t, localvoice.VendorIrodori, localvoice.Irodori.Vendor())
	assert.Equal(t, localvoice.ModelIrodori, localvoice.Irodori.Model())
	assert.Equal(t, domain.ModelGPTSoVITSTTS, localvoice.Local.Model())
	assert.Equal(t, localvoice.RoleVoiceIrodori, localvoice.Irodori.Role())
	assert.Equal(t, localvoice.RoleVoice, localvoice.Local.Role())
}

func TestEmotion_RidesTheContextAndIsEmptyWithoutOne(t *testing.T) {
	assert.Empty(t, localvoice.EmotionOf(context.Background()))
	assert.Equal(t, "緊張", localvoice.EmotionOf(localvoice.WithEmotion(context.Background(), "緊張")))
}

func TestIsWav(t *testing.T) {
	assert.True(t, localvoice.IsWav([]byte("RIFF\x24\x00\x00\x00WAVEfmt ")))
	assert.False(t, localvoice.IsWav([]byte("ID3\x04\x00\x00\x00\x00\x00\x00\x00\x00\x00")))
	assert.False(t, localvoice.IsWav(nil))
}

type preparer struct {
	prepared int
	err      error
}

func (p *preparer) Prepare(context.Context) error { p.prepared++; return p.err }

type checkingPreparer struct {
	preparer
	check error
}

func (c *checkingPreparer) CheckVoices(map[string]localvoice.Voice) error { return c.check }

func TestPrepare_AVoiceThatCanRefuseTheReferencesDoesSoBeforeItIsStarted(t *testing.T) {
	refusal := &localvoice.ReferenceError{Speaker: "ユイ", Problem: "空です", Vendor: localvoice.VendorIrodori}
	c := &checkingPreparer{check: refusal}

	err := localvoice.Prepare(context.Background(), c, nil)

	assert.ErrorIs(t, err, refusal)
	assert.Zero(t, c.prepared)
	ok := &checkingPreparer{}
	require.NoError(t, localvoice.Prepare(context.Background(), ok, nil))
	assert.Equal(t, 1, ok.prepared)
}

func TestPrepare_AVoiceThatCannotCheckIsJustStarted(t *testing.T) {
	boom := errors.New("boom")
	p := &preparer{err: boom}

	err := localvoice.Prepare(context.Background(), p, nil)

	assert.ErrorIs(t, err, boom)
	assert.Equal(t, 1, p.prepared)
}

func TestCheckFor_NamesTheVoiceThatWasAsked(t *testing.T) {
	lines := []domain.VoiceLine{{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "こんにちは"}}

	err := localvoice.CheckFor(localvoice.VendorIrodori, lines, map[string]localvoice.Voice{}, true, func() error { return nil })

	var missing *localvoice.MissingVoiceError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, localvoice.VendorIrodori, missing.Vendor)
	assert.Equal(t, localvoice.RoleVoiceIrodori, missing.Role())
	var gptsovits *localvoice.MissingVoiceError
	err = localvoice.Check(lines, map[string]localvoice.Voice{}, true, func() error { return nil })
	require.ErrorAs(t, err, &gptsovits)
	assert.Equal(t, localvoice.RoleVoice, gptsovits.Role(), "the older entry point is still GPT-SoVITS")
}

func TestCheckFor_AProviderThatIsNotWiredIsUnavailableWithItsOwnVendor(t *testing.T) {
	lines := []domain.VoiceLine{{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "こんにちは"}}
	voices := map[string]localvoice.Voice{"ユイ": {Path: "/a.wav"}}

	err := localvoice.CheckFor(localvoice.VendorIrodori, lines, voices, false, func() error { return nil })

	var ue *localvoice.UnavailableError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, localvoice.VendorIrodori, ue.Vendor)
	assert.Equal(t, localvoice.KindNotInstalled, ue.Kind)
	assert.Contains(t, localvoice.Reason(ue), "Irodori-TTS")
}

func TestReason_IrodoriNamesItselfAndTheConsentOfALicenseHasItsOwnSentence(t *testing.T) {
	for _, kind := range append(everyKind[:6:6], localvoice.KindConsent) {
		text := localvoice.Reason(&localvoice.UnavailableError{Kind: kind, Vendor: localvoice.VendorIrodori})
		assert.Contains(t, text, "Irodori-TTS", kind)
		assert.NotContains(t, text, "GPT-SoVITS", kind)
	}
	consent := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindConsent, Vendor: localvoice.VendorIrodori})
	assert.Contains(t, consent, "ライセンスに同意していない")
	assert.Contains(t, consent, "モデルマネージャー")
	assert.True(t, (&localvoice.UnavailableError{Kind: localvoice.KindConsent}).Systemic(), "no line of the run can be spoken until the license is accepted")
}

func TestSwitched_SaysWhichLocalVoiceWasNotUsed(t *testing.T) {
	got := localvoice.Switched(localvoice.RoleVoiceIrodori, "導入されていません")

	assert.Contains(t, got, "ローカルの声（Irodori-TTS）")
	assert.Contains(t, got, "クラウドの声（ElevenLabs）")
	assert.Contains(t, got, "導入されていません")
}

func TestReferenceError_IsReportedWithTheSpeakerAndTheVoiceThatRefused(t *testing.T) {
	err := &localvoice.ReferenceError{Speaker: "ユイ", Problem: "長すぎます", Vendor: localvoice.VendorIrodori}

	assert.Contains(t, err.Error(), "ユイ")
	assert.Contains(t, err.Error(), "長すぎます")
	assert.Equal(t, localvoice.RoleVoiceIrodori, err.Role())
}
