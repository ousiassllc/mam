package audiosettings_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// With the fallback on, a local voice that this machine cannot run makes the whole run go to the cloud; the switch is told in the chat and
// the cost of the cloud is in the ledger as always.
func TestGenerateAudio_WithTheFallbackAnUnsupportedLocalVoiceMakesTheWholeRunUseTheCloud(t *testing.T) {
	e := newEditorRig(t, fakeKeys{"elevenlabs": true}, false)
	e.rig.rt.refuse["gpt-sovits"] = macOSRefusal("gpt-sovits")
	e.save(audiosettings.Update{VoiceProvider: new(localvoice.Local), FallbackToCloud: new(true)})

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	require.NoError(t, err)
	assert.Equal(t, map[domain.ModelName]int{domain.ModelElevenLabsTTS: 2}, e.speechModels(), "every line is spoken by the cloud: the two voices are not mixed")
	assert.Zero(t, e.local.prepared, "the local runtime is not started")
	assert.Zero(t, e.local.lines)
	assert.Equal(t, 2, e.ledger()[string(domain.ModelElevenLabsTTS)], "the cloud cost is in the ledger")
	assert.Contains(t, e.lastChat(), "ローカルの声（GPT-SoVITS）を使えなかったため、この実行はクラウドの声（ElevenLabs）で作りました", "the switch is in the chat")
	assert.Contains(t, e.lastChat(), "公式の PyTorch 版が使えません")
}

// A speaker without a reference voice is a reason to go to the cloud too (when the user allowed it).
func TestGenerateAudio_WithTheFallbackASpeakerWithoutAReferenceVoiceMakesTheWholeRunUseTheCloud(t *testing.T) {
	e := newEditorRig(t, fakeKeys{"elevenlabs": true}, false)
	e.Exec(`DELETE FROM character WHERE id = 'c-yui'`)
	e.save(audiosettings.Update{VoiceProvider: new(localvoice.Local), FallbackToCloud: new(true)})

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	require.NoError(t, err)
	assert.Equal(t, map[domain.ModelName]int{domain.ModelElevenLabsTTS: 2}, e.speechModels(), "the narration is not left on the local voice")
	assert.Contains(t, e.lastChat(), "参照音声が無い話者がいます")
}

// With the fallback on but no ElevenLabs key, the run stops with the instruction for the key, before anything is started or paid.
func TestGenerateAudio_WithTheFallbackButNoCloudKeyTheRunStopsWithTheInstruction(t *testing.T) {
	e := newEditorRig(t, nil, false)
	e.rig.rt.refuse["gpt-sovits"] = localruntime.ErrNotInstalled
	e.save(audiosettings.Update{VoiceProvider: new(localvoice.Local), FallbackToCloud: new(true)})

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Contains(t, ai.Reason, "elevenlabs のキーを登録")
	assert.Contains(t, ai.Reason, "導入されていません", "and why the local voice cannot be used")
	var nk *localvoice.NoCloudKeyError
	assert.ErrorAs(t, err, &nk)
	assert.Zero(t, e.local.prepared)
	assert.Empty(t, e.ledger())
	assert.Equal(t, "AUDIO", e.State())
}

// A usable local voice is used, with the fallback on as off.
func TestGenerateAudio_WithTheFallbackAUsableLocalVoiceIsUsed(t *testing.T) {
	e := newEditorRig(t, fakeKeys{"elevenlabs": true}, false)
	e.save(audiosettings.Update{VoiceProvider: new(localvoice.Local), FallbackToCloud: new(true)})

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	require.NoError(t, err)
	assert.Equal(t, map[domain.ModelName]int{domain.ModelGPTSoVITSTTS: 2}, e.speechModels())
	assert.NotContains(t, e.lastChat(), "クラウド")
}

// The lip sync goes to Sync Labs for the whole run when the local one cannot be used and the user allowed it; the voice, which can be used,
// stays the local one.
func TestGenerateAudio_WithTheFallbackAnUnusableLocalLipSyncMakesTheRunUseSyncLabs(t *testing.T) {
	e := newEditorRig(t, fakeKeys{"synclabs": true}, true)
	e.rig.rt.refuse["musetalk"] = &localruntime.WeightsError{Runtime: "musetalk", Name: "MuseTalk 1.5", Assets: []rtm.AssetStatus{{Name: "unet.pth", State: rtm.AssetMissing}}}
	e.save(audiosettings.Update{VoiceProvider: new(localvoice.Local), LipSyncProvider: new(localvoice.LipSyncLocal), FallbackToCloud: new(true)})

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	require.NoError(t, err)
	assert.Equal(t, 1, e.sync.calls, "Sync Labs made the clip")
	assert.Equal(t, map[domain.ModelName]int{domain.ModelGPTSoVITSTTS: 2}, e.speechModels())
	assert.Contains(t, e.lastChat(), "ローカルの口パク（MuseTalk 1.5）を使えなかったため、この実行はクラウドの口パク（Sync Labs）で作りました")
	assert.Contains(t, e.lastChat(), "unet.pth")
	assert.Equal(t, 1, e.ledger()[string(domain.ModelSyncLabs)], "the cloud cost is in the ledger")
}

// Without the fallback, a local lip sync that is not installed is the reason of the skipped step, in the words of the settings screen, and
// Sync Labs is not called.
func TestGenerateAudio_WithoutTheFallbackTheLocalLipSyncThatCannotBeUsedIsSkippedWithItsReason(t *testing.T) {
	e := newEditorRig(t, fakeKeys{"synclabs": true}, true)
	e.save(audiosettings.Update{LipSyncProvider: new(localvoice.LipSyncLocal)})

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	require.NoError(t, err)
	assert.Zero(t, e.sync.calls, "no cloud call is made that the user did not choose")
	v, err := e.Editors().LatestAudioTimeline(context.Background(), e.ProjectID, "")
	require.NoError(t, err)
	var reasons []string
	for _, s := range v.Timeline.Skipped {
		if s.Layer == domain.LayerLipSync {
			reasons = append(reasons, s.Detail)
		}
	}
	require.Len(t, reasons, 1)
	assert.Contains(t, reasons[0], "ローカルの口パク（MuseTalk 1.5）が導入されていません")
	assert.Contains(t, reasons[0], "Sync Labs を選ぶか", "and how to use the cloud")
}

// A settings file that cannot be read stops the run with the instruction to fix it: a run does not guess which provider was meant (which
// could cost money the user did not choose).
func TestGenerateAudio_ABrokenSettingsFileStopsTheRun(t *testing.T) {
	e := newEditorRig(t, fakeKeys{"elevenlabs": true}, false)
	require.NoError(t, os.WriteFile(e.rig.path, []byte(`{"voice_provider":`), 0o600))

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Equal(t, audiosettings.UnreadableMessage, ai.Reason)
	assert.Empty(t, e.ledger())
}
