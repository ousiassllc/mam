package audiosettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// With the fallback off a run is made as asked, whatever the local providers are: the Director reports what it finds, and no cloud call is
// made that the user did not choose.
func TestPreflight_WithoutTheFallbackNothingChangesAndNothingIsChecked(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true, "synclabs": true})
	r.rt.refuse["gpt-sovits"], r.rt.refuse["musetalk"] = macOSRefusal("gpt-sovits"), macOSRefusal("musetalk")

	plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{
		Voice: localvoice.Local, LipSync: localvoice.LipSyncLocal, NeedLipSync: true, Voices: map[string]localvoice.Voice{"ユイ": {Path: "/v.wav"}},
	})

	require.NoError(t, err)
	assert.Equal(t, audiosettings.Plan{Voice: localvoice.Local, LipSync: localvoice.LipSyncLocal}, plan)
	assert.Empty(t, r.rt.checks)
}

func TestPreflight_ALocalProviderThatCannotBeUsedGoesToTheCloudForTheWholeRun(t *testing.T) {
	voices := map[string]localvoice.Voice{"ユイ": {Path: "/yui.wav"}, "ナレーション": {Path: "/na.wav"}}
	for name, tc := range map[string]struct {
		refuse error
		want   string
	}{
		"macOS":             {macOSRefusal("x"), "公式の PyTorch 版が使えません"},
		"not installed":     {localruntime.ErrNotInstalled, "導入されていません"},
		"an install runs":   {localruntime.ErrBusy, "導入の最中です"},
		"weights missing":   {&localruntime.WeightsError{Runtime: "x", Name: "X", Assets: []rtm.AssetStatus{{Name: "s2G.pth", State: rtm.AssetMissing}}}, "s2G.pth"},
		"too little memory": {&localruntime.SupportError{Runtime: "x", Support: rtm.Support{Code: rtm.CodeLowVRAM, Reason: "GPU のビデオメモリは 2.0 GB で、必要な 4 GB に足りません。"}}, "必要な 4 GB に足りません"},
	} {
		t.Run("voice, "+name, func(t *testing.T) {
			r := newRig(t, fakeKeys{"elevenlabs": true})
			r.rt.refuse["gpt-sovits"] = tc.refuse

			plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Local, LipSync: localvoice.LipSyncSyncLabs, Fallback: true, Voices: voices})

			require.NoError(t, err)
			assert.Equal(t, localvoice.ElevenLabs, plan.Voice, "every line of the run is spoken by the cloud: no run mixes two voices")
			assert.Equal(t, localvoice.LipSyncSyncLabs, plan.LipSync)
			require.Len(t, plan.Notes, 1)
			assert.Contains(t, plan.Notes[0], "ローカルの声（GPT-SoVITS）を使えなかったため、この実行はクラウドの声（ElevenLabs）で作りました")
			assert.Contains(t, plan.Notes[0], tc.want)
		})
		t.Run("lip sync, "+name, func(t *testing.T) {
			r := newRig(t, fakeKeys{"synclabs": true})
			r.rt.refuse["musetalk"] = tc.refuse

			plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.ElevenLabs, LipSync: localvoice.LipSyncLocal, Fallback: true, NeedLipSync: true})

			require.NoError(t, err)
			assert.Equal(t, localvoice.LipSyncSyncLabs, plan.LipSync)
			assert.Equal(t, localvoice.ElevenLabs, plan.Voice)
			require.Len(t, plan.Notes, 1)
			assert.Contains(t, plan.Notes[0], "ローカルの口パク（MuseTalk 1.5）を使えなかったため、この実行はクラウドの口パク（Sync Labs）で作りました")
			assert.Contains(t, plan.Notes[0], tc.want)
		})
	}
}

func TestPreflight_ASpeakerWithoutAReferenceVoiceMakesTheWholeVoiceGoToTheCloud(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true})
	voices := map[string]localvoice.Voice{"ユイ": {Path: "/yui.wav"}, "ケン": {Cause: errors.New("no reference")}, "アキ": {Cause: errors.New("no reference")}}

	plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Local, Fallback: true, Voices: voices})

	require.NoError(t, err)
	assert.Equal(t, localvoice.ElevenLabs, plan.Voice, "the speaker with a voice is not left on the local one")
	require.Len(t, plan.Notes, 1)
	assert.Contains(t, plan.Notes[0], "参照音声が無い話者がいます: 「アキ」、「ケン」", "all of them, in a fixed order")
	assert.Empty(t, r.rt.checks, "the runtime is not even asked")
}

func TestPreflight_WhenTheLocalProvidersAreUsableNothingChanges(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true, "synclabs": true})

	plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{
		Voice: localvoice.Local, LipSync: localvoice.LipSyncLocal, Fallback: true, NeedLipSync: true, Voices: map[string]localvoice.Voice{"ユイ": {Path: "/yui.wav"}},
	})

	require.NoError(t, err)
	assert.Equal(t, audiosettings.Plan{Voice: localvoice.Local, LipSync: localvoice.LipSyncLocal}, plan)
	assert.ElementsMatch(t, []string{"gpt-sovits", "musetalk"}, r.rt.checks)
}

func TestPreflight_OnlyWhatTheRunNeedsIsChecked(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true, "synclabs": true})
	r.rt.refuse["gpt-sovits"], r.rt.refuse["musetalk"] = macOSRefusal("x"), macOSRefusal("x")

	plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Local, LipSync: localvoice.LipSyncLocal, Fallback: true})

	require.NoError(t, err)
	assert.Equal(t, audiosettings.Plan{Voice: localvoice.Local, LipSync: localvoice.LipSyncLocal}, plan,
		"a script without lines needs no voice, and a Quality Policy without Lip Sync needs no mouths")
	assert.Empty(t, r.rt.checks)
}

func TestPreflight_TheCloudProviderNeedsItsKey(t *testing.T) {
	voices := map[string]localvoice.Voice{"ユイ": {Path: "/yui.wav"}}
	t.Run("voice", func(t *testing.T) {
		r := newRig(t, fakeKeys{"synclabs": true}) // the other provider's key does not count
		r.rt.refuse["gpt-sovits"] = macOSRefusal("x")

		_, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Local, Fallback: true, Voices: voices})

		var nk *localvoice.NoCloudKeyError
		require.ErrorAs(t, err, &nk)
		assert.Equal(t, localvoice.RoleVoice, nk.Role)
		assert.Contains(t, err.Error(), "elevenlabs のキーを登録")
		assert.Contains(t, err.Error(), "公式の PyTorch 版が使えません", "why the local one cannot be used is still said")
	})
	t.Run("lip sync", func(t *testing.T) {
		r := newRig(t, fakeKeys{"elevenlabs": true})
		r.rt.refuse["musetalk"] = macOSRefusal("x")

		_, err := r.svc.Preflight(context.Background(), audiosettings.Run{LipSync: localvoice.LipSyncLocal, Fallback: true, NeedLipSync: true})

		var nk *localvoice.NoCloudKeyError
		require.ErrorAs(t, err, &nk)
		assert.Equal(t, localvoice.RoleLipSync, nk.Role)
		assert.Contains(t, err.Error(), "synclabs のキーを登録")
	})
	t.Run("no key store", func(t *testing.T) {
		r := newRig(t, nil)
		r.rt.refuse["gpt-sovits"] = macOSRefusal("x")

		_, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Local, Fallback: true, Voices: voices})

		var nk *localvoice.NoCloudKeyError
		assert.ErrorAs(t, err, &nk)
	})
}

func TestPreflight_ACancelledCallIsNotAFailureOfTheLocalProvider(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true})
	r.rt.refuse["gpt-sovits"] = macOSRefusal("x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.svc.Preflight(ctx, audiosettings.Run{Voice: localvoice.Local, Fallback: true, Voices: map[string]localvoice.Voice{"ユイ": {Path: "/yui.wav"}}})

	assert.ErrorIs(t, err, context.Canceled)
}
