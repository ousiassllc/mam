package audiosettings_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// fakeRuntimes answers for the two runtimes of the model manager by id.
type fakeRuntimes struct {
	views  map[string]localruntime.View
	refuse map[string]error
	checks []string
}

func (f *fakeRuntimes) Status(_ context.Context, id string) (localruntime.View, error) {
	v, ok := f.views[id]
	if !ok {
		return localruntime.View{}, localruntime.ErrNotFound
	}
	return v, nil
}

func (f *fakeRuntimes) CheckStart(_ context.Context, id string) error {
	f.checks = append(f.checks, id)
	return f.refuse[id]
}

type fakeKeys map[string]bool

func (k fakeKeys) Configured(provider string) (bool, error) { return k[provider], nil }

// usable is a runtime of this machine that can be used: supported, installed, weights verified.
func usable(id, name string) localruntime.View {
	v := localruntime.View{ID: id, Name: name, Support: rtm.Support{Supported: true, Code: rtm.CodeOK}}
	v.Installed = true
	v.Weights.State = localruntime.WeightsReady
	v.VRAM = catalog.VRAM{MinGB: 4}
	return v
}

func macOSRefusal(id string) error {
	return &localruntime.SupportError{Runtime: id, Support: rtm.Support{Code: rtm.CodeOS,
		Reason: "macOS では自動導入できません。公式の PyTorch 版が使えません。", Fallback: "代わりに、クラウドを使ってください。"}}
}

type rig struct {
	svc  *audiosettings.Service
	rt   *fakeRuntimes
	path string
}

func newRig(t *testing.T, keys fakeKeys) *rig {
	t.Helper()
	rt := &fakeRuntimes{
		views:  map[string]localruntime.View{"gpt-sovits": usable("gpt-sovits", "GPT-SoVITS"), "musetalk": usable("musetalk", "MuseTalk 1.5")},
		refuse: map[string]error{},
	}
	path := filepath.Join(t.TempDir(), audiosettings.FileName)
	return &rig{svc: audiosettings.New(path, rt, keys), rt: rt, path: path}
}

func TestLoad_WithoutAFileEveryRunUsesTheCloud(t *testing.T) {
	r := newRig(t, nil)

	got, err := r.svc.Load()

	require.NoError(t, err)
	assert.Equal(t, audiosettings.Settings{VoiceProvider: localvoice.ElevenLabs, LipSyncProvider: localvoice.LipSyncSyncLabs}, got)
	assert.False(t, got.FallbackToCloud, "the fallback is off until the user turns it on")
	assert.NoFileExists(t, r.path)
}

func TestSave_SurvivesARestartAndKeepsWhatItDoesNotManage(t *testing.T) {
	r := newRig(t, nil)
	require.NoError(t, os.WriteFile(r.path, []byte(`{"note":"by hand","voice_provider":"elevenlabs"}`), 0o600))

	require.NoError(t, r.svc.Save(context.Background(), audiosettings.Update{VoiceProvider: new(localvoice.Local), FallbackToCloud: new(true)}))

	again := audiosettings.New(r.path, r.rt, nil) // a new process reads the same file
	got, err := again.Load()
	require.NoError(t, err)
	assert.Equal(t, audiosettings.Settings{VoiceProvider: localvoice.Local, LipSyncProvider: localvoice.LipSyncSyncLabs, FallbackToCloud: true}, got,
		"a choice that was not named keeps what was there")
	raw, err := os.ReadFile(r.path)
	require.NoError(t, err)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(raw, &obj))
	assert.Equal(t, "by hand", obj["note"])
	info, err := os.Stat(r.path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestSave_ALocalProviderThatThisMachineCannotRunIsRefusedWithItsReason(t *testing.T) {
	for name, tc := range map[string]struct {
		update audiosettings.Update
		id     string
		field  string
		want   string
	}{
		"voice":    {audiosettings.Update{VoiceProvider: new(localvoice.Local)}, "gpt-sovits", "voice_provider", "ローカルの声（GPT-SoVITS）を選べません"},
		"lip sync": {audiosettings.Update{LipSyncProvider: new(localvoice.LipSyncLocal)}, "musetalk", "lip_sync_provider", "ローカルの口パク（MuseTalk 1.5）を選べません"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, nil)
			v := r.rt.views[tc.id]
			v.Support = rtm.Support{Code: rtm.CodeOS, Reason: "macOS では公式の PyTorch 版が使えません。", Fallback: "クラウドを使ってください。"}
			r.rt.views[tc.id] = v

			err := r.svc.Save(context.Background(), tc.update)

			var invalid *audiosettings.InvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tc.field, invalid.Field)
			assert.Contains(t, invalid.Message, tc.want)
			assert.Contains(t, invalid.Message, "macOS では公式の PyTorch 版が使えません", "the reason of the runtime manager")
			assert.Contains(t, invalid.Message, "クラウドを使ってください", "and what to use instead")
			assert.NoFileExists(t, r.path, "nothing is written")
		})
	}
}

func TestSave_TheCloudProvidersAreChoosableAnywhere(t *testing.T) {
	r := newRig(t, nil)
	v := r.rt.views["gpt-sovits"]
	v.Support = rtm.Support{Code: rtm.CodeOS, Reason: "x"}
	r.rt.views["gpt-sovits"] = v

	require.NoError(t, r.svc.Save(context.Background(), audiosettings.Update{VoiceProvider: new(localvoice.ElevenLabs), LipSyncProvider: new(localvoice.LipSyncSyncLabs)}))
}

func TestSave_AnUnknownProviderIsInvalid(t *testing.T) {
	r := newRig(t, nil)
	for name, tc := range map[string]struct {
		update audiosettings.Update
		field  string
	}{
		"voice":             {audiosettings.Update{VoiceProvider: new(localvoice.Provider("azure"))}, "voice_provider"},
		"empty voice":       {audiosettings.Update{VoiceProvider: new(localvoice.Provider(""))}, "voice_provider"},
		"lip sync":          {audiosettings.Update{LipSyncProvider: new(localvoice.LipSyncProvider("heygen"))}, "lip_sync_provider"},
		"lip sync, in caps": {audiosettings.Update{LipSyncProvider: new(localvoice.LipSyncProvider("Local"))}, "lip_sync_provider"},
		"empty lip sync":    {audiosettings.Update{LipSyncProvider: new(localvoice.LipSyncProvider(""))}, "lip_sync_provider"},
	} {
		t.Run(name, func(t *testing.T) {
			err := r.svc.Save(context.Background(), tc.update)

			var invalid *audiosettings.InvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tc.field, invalid.Field)
			assert.NoFileExists(t, r.path)
		})
	}
}

func TestSave_WithoutLocalRuntimesTheLocalProvidersCannotBeChosen(t *testing.T) {
	svc := audiosettings.New(filepath.Join(t.TempDir(), audiosettings.FileName), nil, nil)

	err := svc.Save(context.Background(), audiosettings.Update{VoiceProvider: new(localvoice.Local)})

	var invalid *audiosettings.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "このアプリでは使えません")
	require.NoError(t, svc.Save(context.Background(), audiosettings.Update{FallbackToCloud: new(true)}))
}

func TestLoad_AFileThatCannotBeReadIsAnErrorNotAGuess(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":                `{"voice_provider": `,
		"not an object":           `[1]`,
		"a provider that is none": `{"voice_provider":"azure"}`,
		"an empty provider":       `{"lip_sync_provider":""}`,
		"the wrong type":          `{"fallback_to_cloud":"yes"}`,
		"a lip sync of the voice": `{"lip_sync_provider":"elevenlabs"}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, nil)
			require.NoError(t, os.WriteFile(r.path, []byte(content), 0o600))

			_, err := r.svc.Load()
			assert.ErrorIs(t, err, audiosettings.ErrUnreadable)

			saveErr := r.svc.Save(context.Background(), audiosettings.Update{FallbackToCloud: new(true)})
			assert.ErrorIs(t, saveErr, audiosettings.ErrUnreadable, "a broken file is not overwritten")
			raw, readErr := os.ReadFile(r.path)
			require.NoError(t, readErr)
			assert.Equal(t, content, string(raw))
		})
	}
}
