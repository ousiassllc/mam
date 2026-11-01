package audiosettings_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editortest"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// localSpeech is the local voice: it answers with a tone and counts what it was asked.
type localSpeech struct {
	data []byte

	mu         sync.Mutex
	prepared   int
	lines      int
	prepareErr error
}

func (l *localSpeech) Prepare(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prepared++
	return l.prepareErr
}

func (l *localSpeech) Synthesize(context.Context, domain.VoiceLine, localvoice.Voice) (audiodirector.Speech, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines++
	return audiodirector.Speech{Data: l.data, Ext: "wav"}, nil
}

// cloudSync is Sync Labs: it hands the clip back as it was and counts the calls.
type cloudSync struct {
	mu    sync.Mutex
	calls int
}

func (c *cloudSync) Sync(_ context.Context, req audiodirector.LipSyncRequest) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return req.Video.Data, nil
}

// localLip is the local lip sync: it cannot be prepared (the runtime is not installed).
type localLip struct {
	cloudSync
	prepareErr error
}

func (l *localLip) Prepare(context.Context) error { return l.prepareErr }

// editorRig is the editor fixture with both local providers, a cloud lip sync, a script of a narration and a line of ユイ, reference voices
// for both, the runtimes of the manager answering through rt and the settings file at the path of the rig.
type editorRig struct {
	*editortest.Fixture
	rig   *rig
	local *localSpeech
	sync  *cloudSync
	lip   *localLip
}

func newEditorRig(t *testing.T, keys fakeKeys, maxQuality bool) *editorRig {
	t.Helper()
	r := newRig(t, keys)
	local := &localSpeech{data: testenv.FFmpegFile(t, ".wav", "-f", "lavfi", "-i", "sine=frequency=300:duration=1.5")}
	cloud := &cloudSync{}
	lip := &localLip{prepareErr: &localvoice.UnavailableError{Kind: localvoice.KindNotInstalled, Vendor: localvoice.VendorMuseTalk}}
	f := editortest.New(t, func(*mcptest.Env, *mcpkit.Registrar) {}, func(d *service.Deps) {
		d.Audio.LocalSpeech, d.Audio.LipSync, d.Audio.LocalLipSync = local, cloud, lip
		d.AudioSettings = r.svc
	})
	f.WithScript()
	if maxQuality {
		f.Exec(`UPDATE project SET quality_policy = 'max_quality' WHERE id = ?`, f.ProjectID)
	}
	ctx := context.Background()
	v, err := f.Services.Editor.RoughCut(ctx, f.ProjectID, "")
	require.NoError(t, err)
	_, err = f.Services.Editor.JudgeTimeline(ctx, v.Timeline.ID)
	require.NoError(t, err)
	require.Equal(t, "AUDIO", f.State())
	e := &editorRig{Fixture: f, rig: r, local: local, sync: cloud, lip: lip}
	e.setVoice("c-yui", "ユイ")
	e.setVoice("c-na", voiceref.NarrationName)
	return e
}

func (e *editorRig) setVoice(id, name string) {
	e.T.Helper()
	rec := testenv.FFmpegFile(e.T, ".wav", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=5", 300+len(name)))
	e.Exec(`INSERT INTO character (id, project_id, name) VALUES (?, ?, ?)`, id, e.ProjectID, name)
	text := ""
	_, err := e.Services.VoiceRef.SetForProject(context.Background(), e.ProjectID, id, voicerefsvc.Input{
		File: voicerefsvc.File{Name: "ref.wav", Size: int64(len(rec)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(rec)), nil }},
		Text: &text,
	})
	require.NoError(e.T, err)
}

func (e *editorRig) save(u audiosettings.Update) {
	e.T.Helper()
	require.NoError(e.T, e.rig.svc.Save(context.Background(), u))
}

func (e *editorRig) ledger() map[string]int {
	e.T.Helper()
	rows, err := e.DB.Reader().QueryContext(context.Background(), `SELECT provider FROM cost_ledger_entry WHERE project_id = ?`, e.ProjectID)
	require.NoError(e.T, err)
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var p string
		require.NoError(e.T, rows.Scan(&p))
		out[p]++
	}
	require.NoError(e.T, rows.Err())
	return out
}

func (e *editorRig) lastChat() string {
	e.T.Helper()
	var content string
	require.NoError(e.T, e.DB.Reader().QueryRow(`SELECT content FROM chat_message WHERE project_id = ? AND role = 'assistant' ORDER BY created_at DESC, rowid DESC LIMIT 1`, e.ProjectID).Scan(&content))
	return content
}

func (e *editorRig) speechModels() map[domain.ModelName]int {
	e.T.Helper()
	v, err := e.Editors().LatestAudioTimeline(context.Background(), e.ProjectID, "")
	require.NoError(e.T, err)
	out := map[domain.ModelName]int{}
	for _, c := range v.Timeline.Clips {
		if c.LayerOf() == domain.LayerDialogue || c.LayerOf() == domain.LayerNarration {
			out[c.Model]++
		}
	}
	return out
}

// Nothing saved: the run is made by ElevenLabs, as it was before the local providers existed, and the local runtime is never asked.
func TestGenerateAudio_WithoutSavedSettingsTheRunIsMadeByTheCloud(t *testing.T) {
	e := newEditorRig(t, fakeKeys{"elevenlabs": true}, false)

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	require.NoError(t, err)
	assert.Equal(t, map[domain.ModelName]int{domain.ModelElevenLabsTTS: 2}, e.speechModels())
	assert.Zero(t, e.local.prepared)
	assert.Empty(t, e.rig.rt.checks)
}

// The choice is read at every run, so a saved choice applies to the next run without a restart.
func TestGenerateAudio_TheSavedChoiceIsReadAtEveryRun(t *testing.T) {
	e := newEditorRig(t, nil, false)
	ctx := context.Background()

	e.save(audiosettings.Update{VoiceProvider: new(localvoice.Local)})
	_, err := e.Services.Editor.GenerateAudio(ctx, e.ProjectID, "")
	require.NoError(t, err)
	assert.Equal(t, map[domain.ModelName]int{domain.ModelGPTSoVITSTTS: 2}, e.speechModels())
	assert.Equal(t, 2, e.local.lines)

	e.save(audiosettings.Update{VoiceProvider: new(localvoice.ElevenLabs)})
	_, err = e.Services.Editor.GenerateAudio(ctx, e.ProjectID, "")
	require.NoError(t, err)
	assert.Equal(t, map[domain.ModelName]int{domain.ModelElevenLabsTTS: 2}, e.speechModels())
	assert.Equal(t, 2, e.local.lines, "the local voice speaks nothing more")
}

// A run that names its providers (the options of GenerateAudioWith) is made as asked, whatever is saved.
func TestGenerateAudioWith_ExplicitOptionsWinOverTheSavedChoice(t *testing.T) {
	e := newEditorRig(t, nil, false)
	e.save(audiosettings.Update{VoiceProvider: new(localvoice.Local)})

	_, err := e.Services.Editor.GenerateAudioWith(context.Background(), e.ProjectID, "", service.AudioOptions{VoiceProvider: localvoice.ElevenLabs})

	require.NoError(t, err)
	assert.Equal(t, map[domain.ModelName]int{domain.ModelElevenLabsTTS: 2}, e.speechModels())
}

// Without the fallback a local voice that cannot be used stops the run with its reason, and no cloud call is made (no API cost appears).
func TestGenerateAudio_WithoutTheFallbackALocalVoiceThatCannotBeUsedStopsTheRun(t *testing.T) {
	e := newEditorRig(t, fakeKeys{"elevenlabs": true}, false)
	e.local.prepareErr = &localvoice.UnavailableError{Kind: localvoice.KindUnsupported, Detail: "macOS では公式の PyTorch 版が使えません"}
	e.save(audiosettings.Update{VoiceProvider: new(localvoice.Local)})

	_, err := e.Services.Editor.GenerateAudio(context.Background(), e.ProjectID, "")

	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Contains(t, ai.Reason, "この PC ではローカルの声（GPT-SoVITS）を使えません（macOS では公式の PyTorch 版が使えません）")
	assert.Contains(t, ai.Reason, "設定の「音声・口パク」", "and how to use the cloud")
	assert.Empty(t, e.ledger(), "the cloud is never used without the setting: no API cost appears")
	assert.Empty(t, e.rig.rt.checks, "the preflight is for the fallback")
	assert.Equal(t, "AUDIO", e.State())
}
