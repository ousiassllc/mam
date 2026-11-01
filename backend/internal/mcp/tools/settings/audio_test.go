package settings_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

type runtimes struct {
	views  map[string]localruntime.View
	refuse map[string]error
}

func (r runtimes) Status(_ context.Context, id string) (localruntime.View, error) {
	return r.views[id], nil
}
func (r runtimes) CheckStart(_ context.Context, id string) error { return r.refuse[id] }

type keys map[string]bool

func (k keys) Configured(provider string) (bool, error) { return k[provider], nil }

func machine(supported bool) runtimes {
	view := func(id, name string) localruntime.View {
		v := localruntime.View{ID: id, Name: name, SourceURL: "https://example.test/" + id, VRAM: catalog.VRAM{MinGB: 4, Estimate: id == "gpt-sovits"}}
		v.License = catalog.License{Name: "MIT"}
		if supported {
			v.Support = rtm.Support{Supported: true, Code: rtm.CodeOK}
			v.Installed = true
			v.Weights.State = localruntime.WeightsReady
		} else {
			v.Support = rtm.Support{Code: rtm.CodeOS, Reason: "macOS では公式の PyTorch 版が使えません。", Fallback: "代わりに、クラウドを使ってください。"}
		}
		return v
	}
	r := runtimes{views: map[string]localruntime.View{"gpt-sovits": view("gpt-sovits", "GPT-SoVITS"), "musetalk": view("musetalk", "MuseTalk 1.5")}, refuse: map[string]error{}}
	if !supported {
		r.refuse["gpt-sovits"] = &localruntime.SupportError{Runtime: "gpt-sovits", Support: r.views["gpt-sovits"].Support}
		r.refuse["musetalk"] = &localruntime.SupportError{Runtime: "musetalk", Support: r.views["musetalk"].Support}
	}
	return r
}

func audioEnv(t *testing.T, rt audiosettings.Runtimes, k audiosettings.Keys) (*mcptest.Env, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), audiosettings.FileName)
	svc := audiosettings.New(path, rt, k)
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		r.Settings = mcpkit.Settings{Audio: svc}
		settings.Register(r, env.Services)
	}), path
}

// Nothing saved: the cloud providers, the fallback off, the local ones with their state.
func TestGetAudioProviders_DefaultsToTheCloudAndShowsTheLocalState(t *testing.T) {
	env, _ := audioEnv(t, machine(true), keys{"elevenlabs": true})

	var got settings.AudioProvidersSection
	env.CallOK("get_audio_providers", map[string]any{}, &got)

	assert.True(t, got.Available)
	assert.Equal(t, "elevenlabs", got.VoiceProvider)
	assert.Equal(t, "synclabs", got.LipSyncProvider)
	assert.False(t, got.FallbackToCloud, "off until the user turns it on")
	assert.Equal(t, "ready", got.Voice.State)
	assert.True(t, got.Voice.Usable)
	assert.Equal(t, "GPT-SoVITS", got.Voice.Name)
	assert.True(t, got.Voice.VRAMEstimate, "the figure is mam's guess")
	assert.Equal(t, "elevenlabs", got.Voice.CloudProvider)
	assert.True(t, got.Voice.CloudKeySet)
	assert.Equal(t, "synclabs", got.LipSync.CloudProvider)
	assert.False(t, got.LipSync.CloudKeySet, "the key itself is never returned, only whether it is stored")
}

// A machine that cannot run the local providers (macOS): the reason is in the answer and the local one cannot be chosen.
func TestSetAudioProviders_ALocalProviderThatThisMachineCannotRunIsAValidationErrorWithItsReason(t *testing.T) {
	env, path := audioEnv(t, machine(false), nil)

	var got settings.AudioProvidersSection
	env.CallOK("get_audio_providers", map[string]any{}, &got)
	assert.Equal(t, "unsupported", got.Voice.State)
	assert.False(t, got.Voice.Usable)
	assert.Contains(t, got.Voice.SupportReason, "macOS では公式の PyTorch 版が使えません")
	assert.Contains(t, got.Voice.Reason, "この PC ではローカルの声（GPT-SoVITS）を使えません", "the sentence of a failed run")

	msg := env.CallError("set_audio_providers", map[string]any{"voice_provider": "local"})
	assert.Contains(t, msg, "validation:")
	assert.Contains(t, msg, "macOS では公式の PyTorch 版が使えません")
	msg = env.CallError("set_audio_providers", map[string]any{"lip_sync_provider": "local"})
	assert.Contains(t, msg, "validation:")
	assert.NoFileExists(t, path)

	env.CallOK("set_audio_providers", map[string]any{"voice_provider": "elevenlabs", "lip_sync_provider": "synclabs"}, &got)
	assert.Equal(t, "elevenlabs", got.VoiceProvider, "the cloud providers can be chosen anywhere")
}

func TestSetAudioProviders_SavesOnlyWhatIsNamedAndTheChoiceIsRead(t *testing.T) {
	env, path := audioEnv(t, machine(true), keys{"elevenlabs": true, "synclabs": true})

	var got settings.AudioProvidersSection
	env.CallOK("set_audio_providers", map[string]any{"voice_provider": "local"}, &got)
	assert.Equal(t, "local", got.VoiceProvider)
	assert.Equal(t, "synclabs", got.LipSyncProvider, "what is not named keeps what was there")
	assert.False(t, got.FallbackToCloud)

	env.CallOK("set_audio_providers", map[string]any{"fallback_to_cloud": true, "lip_sync_provider": "local"}, &got)
	assert.Equal(t, "local", got.VoiceProvider)
	assert.Equal(t, "local", got.LipSyncProvider)
	assert.True(t, got.FallbackToCloud)
	assert.FileExists(t, path)

	var again settings.AudioProvidersSection
	env.CallOK("get_audio_providers", map[string]any{}, &again)
	assert.Equal(t, got, again, "get shows what set saved")

	env.CallOK("set_audio_providers", map[string]any{"fallback_to_cloud": false}, &got)
	assert.False(t, got.FallbackToCloud, "false is a value, not a missing field")
	assert.Equal(t, "local", got.VoiceProvider)
}

func TestSetAudioProviders_RefusesWhatIsNotAProvider(t *testing.T) {
	env, path := audioEnv(t, machine(true), nil)

	assert.Contains(t, env.CallError("set_audio_providers", map[string]any{}), "validation:")
	assert.Contains(t, env.CallError("set_audio_providers", map[string]any{"voice_provider": "azure"}), "validation:")
	assert.Contains(t, env.CallError("set_audio_providers", map[string]any{"voice_provider": ""}), "validation:")
	assert.Contains(t, env.CallError("set_audio_providers", map[string]any{"lip_sync_provider": "elevenlabs"}), "validation:")
	assert.NoFileExists(t, path)
}

func TestAudioProviders_ABrokenFileIsAProblemAndIsNotOverwritten(t *testing.T) {
	env, path := audioEnv(t, machine(true), nil)
	require.NoError(t, os.WriteFile(path, []byte(`{"voice_provider":`), 0o600))

	var got settings.AudioProvidersSection
	env.CallOK("get_audio_providers", map[string]any{}, &got)
	assert.Equal(t, audiosettings.UnreadableMessage, got.Problem)
	assert.Equal(t, "elevenlabs", got.VoiceProvider, "the defaults are shown")

	assert.Contains(t, env.CallError("set_audio_providers", map[string]any{"voice_provider": "local"}), "conflict:")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, `{"voice_provider":`, string(raw))
}

func TestAudioProviders_WithoutTheLocalRuntimesTheyAreNotAvailable(t *testing.T) {
	env, _ := audioEnv(t, nil, nil)

	var got settings.AudioProvidersSection
	env.CallOK("get_audio_providers", map[string]any{}, &got)

	assert.True(t, got.Available)
	assert.False(t, got.Voice.Available)
	assert.False(t, got.LipSync.Available)
	assert.Equal(t, "elevenlabs", got.Voice.CloudProvider)
	assert.Contains(t, env.CallError("set_audio_providers", map[string]any{"voice_provider": "local"}), "このアプリでは使えません")
}
