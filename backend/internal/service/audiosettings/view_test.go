package audiosettings_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

func TestView_ShowsWhereEachLocalProviderStandsAndWhyItCannotBeUsed(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true})
	voice := r.rt.views["gpt-sovits"]
	voice.Process.State = proc.StateRunning
	voice.SourceURL = "https://github.com/RVC-Boss/GPT-SoVITS"
	voice.License = catalog.License{Name: "MIT", URL: "https://example.test/mit"}
	voice.Weights.Assets = []localruntime.AssetView{
		{Name: "s2G.pth", License: catalog.ResourceLicense{CommercialUse: catalog.CommercialYes}},
		{Name: "chinese-hubert-base", License: catalog.ResourceLicense{CommercialUse: catalog.CommercialUnclear}},
	}
	r.rt.views["gpt-sovits"] = voice
	lip := r.rt.views["musetalk"]
	lip.Support = rtm.Support{Code: rtm.CodeOS, Reason: "macOS では公式の PyTorch 版が使えません。", Fallback: "Sync Labs を使ってください。"}
	r.rt.views["musetalk"] = lip
	r.rt.refuse["musetalk"] = macOSRefusal("musetalk")
	require.NoError(t, r.svc.Save(context.Background(), audiosettings.Update{VoiceProvider: new(localvoice.Local)}))

	v, err := r.svc.View(context.Background())

	require.NoError(t, err)
	assert.Empty(t, v.Problem)
	assert.Equal(t, localvoice.Local, v.Settings.VoiceProvider)
	require.NotNil(t, v.Voice)
	assert.Equal(t, audiosettings.StateRunning, v.Voice.State)
	assert.True(t, v.Voice.Usable)
	assert.Empty(t, v.Voice.Reason)
	assert.Equal(t, []string{"chinese-hubert-base"}, v.Voice.Unclear, "a weight whose commercial use is not clear is named")
	assert.Equal(t, "MIT", v.Voice.LicenseName)
	assert.Equal(t, "https://github.com/RVC-Boss/GPT-SoVITS", v.Voice.SourceURL)
	assert.True(t, v.VoiceKeySet)
	require.NotNil(t, v.LipSync)
	assert.Equal(t, audiosettings.StateUnsupported, v.LipSync.State)
	assert.False(t, v.LipSync.Usable)
	assert.Contains(t, v.LipSync.SupportReason, "公式の PyTorch 版が使えません")
	assert.Contains(t, v.LipSync.Reason, "この PC ではローカルの口パク（MuseTalk 1.5）を使えません")
	assert.False(t, v.LipSyncKeySet)
}

// The sentence of the screen and the MCP tools is the sentence of the run: one function writes both.
func TestView_TheReasonIsTheSentenceAPreflightSays(t *testing.T) {
	r := newRig(t, fakeKeys{"elevenlabs": true})
	r.rt.refuse["gpt-sovits"] = localruntime.ErrNotInstalled
	v := r.rt.views["gpt-sovits"]
	v.Installed = false
	r.rt.views["gpt-sovits"] = v

	view, err := r.svc.View(context.Background())
	require.NoError(t, err)
	plan, err := r.svc.Preflight(context.Background(), audiosettings.Run{Voice: localvoice.Local, Fallback: true, Voices: map[string]localvoice.Voice{"a": {Path: "/a.wav"}}})
	require.NoError(t, err)

	assert.Equal(t, audiosettings.StateNotInstalled, view.Voice.State)
	require.Len(t, plan.Notes, 1)
	assert.Contains(t, plan.Notes[0], view.Voice.Reason)
}

func TestView_States(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*localruntime.View)
		want   audiosettings.State
	}{
		"installing":      {func(v *localruntime.View) { v.InstallJob = "j1"; v.Installed = false }, audiosettings.StateInstalling},
		"not installed":   {func(v *localruntime.View) { v.Installed = false }, audiosettings.StateNotInstalled},
		"weights missing": {func(v *localruntime.View) { v.Weights.State = localruntime.WeightsMissing }, audiosettings.StateWeightsMissing},
		"unverified":      {func(v *localruntime.View) { v.Weights.State = localruntime.WeightsUnverified }, audiosettings.StateWeightsMissing},
		"stopped":         {func(*localruntime.View) {}, audiosettings.StateReady},
		"starting":        {func(v *localruntime.View) { v.Process.State = proc.StateStarting }, audiosettings.StateStarting},
		"running":         {func(v *localruntime.View) { v.Process.State = proc.StateRunning }, audiosettings.StateRunning},
		"failed":          {func(v *localruntime.View) { v.Process.State = proc.StateFailed }, audiosettings.StateFailed},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, nil)
			v := r.rt.views["musetalk"]
			tc.change(&v)
			r.rt.views["musetalk"] = v

			view, err := r.svc.View(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tc.want, view.LipSync.State)
		})
	}
}

func TestView_AFileThatCannotBeReadIsShownAsAProblemWithTheDefaults(t *testing.T) {
	r := newRig(t, nil)
	require.NoError(t, os.WriteFile(r.path, []byte(`{"voice_provider":"azure"}`), 0o600))

	v, err := r.svc.View(context.Background())

	require.NoError(t, err)
	assert.Equal(t, audiosettings.UnreadableMessage, v.Problem)
	assert.Equal(t, audiosettings.Defaults(), v.Settings)
}

func TestView_WithoutLocalRuntimesThereAreNoLocalProviders(t *testing.T) {
	svc := audiosettings.New(filepath.Join(t.TempDir(), audiosettings.FileName), nil, fakeKeys{"elevenlabs": true})

	v, err := svc.View(context.Background())

	require.NoError(t, err)
	assert.Nil(t, v.Voice)
	assert.Nil(t, v.LipSync)
	assert.True(t, v.VoiceKeySet)
}
