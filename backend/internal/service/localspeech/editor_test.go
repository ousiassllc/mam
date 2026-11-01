package localspeech_test

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
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// localSpeech is the local voice: it answers with a tone and remembers the voice each line was spoken in.
type localSpeech struct {
	data []byte

	mu       sync.Mutex
	prepared int
	prepare  error
	lines    []domain.VoiceLine
	voices   []localvoice.Voice
}

func (l *localSpeech) Prepare(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prepared++
	return l.prepare
}

func (l *localSpeech) Synthesize(_ context.Context, line domain.VoiceLine, voice localvoice.Voice) (audiodirector.Speech, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines, l.voices = append(l.lines, line), append(l.voices, voice)
	return audiodirector.Speech{Data: l.data, Ext: "wav"}, nil
}

// atAudio is the editor fixture with the local voice, a script of two lines (ナレーション, ユイ) and the Project at AUDIO.
func atAudio(t *testing.T) (*editortest.Fixture, *localSpeech) {
	t.Helper()
	local := &localSpeech{data: testenv.FFmpegFile(t, ".wav", "-f", "lavfi", "-i", "sine=frequency=300:duration=1.5")}
	f := editortest.New(t, func(*mcptest.Env, *mcpkit.Registrar) {}, func(d *service.Deps) { d.Audio.LocalSpeech = local })
	f.WithScript()
	ctx := context.Background()
	v, err := f.Services.Editor.RoughCut(ctx, f.ProjectID, "")
	require.NoError(t, err)
	_, err = f.Services.Editor.JudgeTimeline(ctx, v.Timeline.ID)
	require.NoError(t, err)
	require.Equal(t, "AUDIO", f.State())
	return f, local
}

func ledger(t *testing.T, f *editortest.Fixture) map[string][]float64 {
	t.Helper()
	rows, err := f.DB.Reader().QueryContext(context.Background(), `SELECT provider, COALESCE(estimated_cost, 0) FROM cost_ledger_entry WHERE project_id = ?`, f.ProjectID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	out := map[string][]float64{}
	for rows.Next() {
		var p string
		var c float64
		require.NoError(t, rows.Scan(&p, &c))
		out[p] = append(out[p], c)
	}
	require.NoError(t, rows.Err())
	return out
}

// setVoice makes a Character of the Project and gives it a reference voice (a 5 s recording).
func setVoice(t *testing.T, f *editortest.Fixture, id, name string, rec []byte, text string) {
	t.Helper()
	f.Exec(`INSERT INTO character (id, project_id, name) VALUES (?, ?, ?)`, id, f.ProjectID, name)
	_, err := f.Services.VoiceRef.SetForProject(context.Background(), f.ProjectID, id, voicerefsvc.Input{
		File: voicerefsvc.File{Name: "ref.wav", Size: int64(len(rec)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(rec)), nil }},
		Text: &text,
	})
	require.NoError(t, err)
}

func sine(t *testing.T, hz int) []byte {
	t.Helper()
	return testenv.FFmpegFile(t, ".wav", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=5", hz))
}

func TestEditor_LocalVoiceSpeaksTheLinesInTheReferenceVoiceOfTheirCharacters(t *testing.T) {
	f, local := atAudio(t)
	setVoice(t, f, "c-yui", "ユイ", sine(t, 400), "ユイです")
	setVoice(t, f, "c-na", voiceref.NarrationName, sine(t, 200), "")

	_, err := f.Services.Editor.GenerateAudioWith(context.Background(), f.ProjectID, "", service.AudioOptions{VoiceProvider: localvoice.Local})

	require.NoError(t, err)
	require.Len(t, local.lines, 2)
	assert.Equal(t, 1, local.prepared)
	for i, line := range local.lines {
		assert.FileExists(t, local.voices[i].Path)
		if line.Kind == domain.VoiceNarration {
			assert.Equal(t, "", local.voices[i].Text)
			continue
		}
		assert.Equal(t, "ユイです", local.voices[i].Text)
		assert.Equal(t, "ja", local.voices[i].Language)
	}
	assert.NotEqual(t, local.voices[0].Path, local.voices[1].Path, "the narration has the voice of its own Character")
	latest, err := f.Editors().LatestAudioTimeline(context.Background(), f.ProjectID, "")
	require.NoError(t, err)
	var speech int
	for _, c := range latest.Timeline.Clips {
		if c.LayerOf() == domain.LayerDialogue || c.LayerOf() == domain.LayerNarration {
			speech++
			assert.Equal(t, domain.ModelGPTSoVITSTTS, c.Model)
		}
	}
	assert.Equal(t, 2, speech)
	// The ledger keeps the lines at no cost, and no ElevenLabs TTS is in it.
	got := ledger(t, f)
	assert.Equal(t, []float64{0, 0}, got[string(domain.ModelGPTSoVITSTTS)])
	assert.NotContains(t, got, string(domain.ModelElevenLabsTTS))
}

// A speaker without a reference voice stops the run before the runtime is started and before anything is paid, and the error names the speaker.
func TestEditor_LocalVoiceRefusesASpeakerWithoutAReferenceVoiceBeforeAnythingIsStartedOrPaid(t *testing.T) {
	f, local := atAudio(t)
	setVoice(t, f, "c-na", voiceref.NarrationName, sine(t, 200), "")

	_, err := f.Services.Editor.GenerateAudioWith(context.Background(), f.ProjectID, "", service.AudioOptions{VoiceProvider: localvoice.Local})

	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Contains(t, ai.Reason, "「ユイ」")
	assert.NotContains(t, ai.Reason, "「ナレーション」", "the narration has its voice")
	var mv *localvoice.MissingVoiceError
	assert.ErrorAs(t, err, &mv)
	assert.Zero(t, local.prepared, "the runtime is not started")
	assert.Empty(t, local.lines)
	assert.Empty(t, ledger(t, f), "nothing is recorded")
	assert.Equal(t, "AUDIO", f.State())
}

// The local voice that cannot be used is told to the user by its kind, and nothing is paid for.
func TestEditor_LocalVoiceThatCannotBeUsedIsAnAIErrorOfItsKind(t *testing.T) {
	for kind, want := range map[localvoice.Kind]string{
		localvoice.KindNotInstalled:   "導入されていません",
		localvoice.KindWeightsMissing: "重みが取得されていません",
		localvoice.KindUnsupported:    "使えません",
		localvoice.KindGPUMemory:      "GPU のメモリ",
		localvoice.KindProcess:        "動きませんでした",
		localvoice.KindTimeout:        "時間内に終わりませんでした",
	} {
		t.Run(string(kind), func(t *testing.T) {
			f, local := atAudio(t)
			local.prepare = &localvoice.UnavailableError{Kind: kind, Detail: "詳細"}
			rec := sine(t, 400)
			setVoice(t, f, "c-yui", "ユイ", rec, "")
			setVoice(t, f, "c-na", voiceref.NarrationName, rec, "")

			_, err := f.Services.Editor.GenerateAudioWith(context.Background(), f.ProjectID, "", service.AudioOptions{VoiceProvider: localvoice.Local})

			var ai *service.AIError
			require.ErrorAs(t, err, &ai)
			assert.Contains(t, ai.Reason, want)
			assert.Contains(t, ai.Reason, "詳細")
			var un *localvoice.UnavailableError
			require.ErrorAs(t, err, &un)
			assert.Equal(t, kind, un.Kind)
			assert.Empty(t, ledger(t, f))
		})
	}
}

func TestEditor_AnUnknownVoiceProviderIsAValidationError(t *testing.T) {
	f, local := atAudio(t)

	_, err := f.Services.Editor.GenerateAudioWith(context.Background(), f.ProjectID, "", service.AudioOptions{VoiceProvider: "azure"})

	var ve *service.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "voice_provider", ve.Field)
	assert.Zero(t, local.prepared)
}

// GenerateAudio, which does not choose, still speaks with ElevenLabs and does not start the local runtime.
func TestEditor_GenerateAudioStillSpeaksWithElevenLabs(t *testing.T) {
	f, local := atAudio(t)

	_, err := f.Services.Editor.GenerateAudio(context.Background(), f.ProjectID, "")

	require.NoError(t, err)
	assert.Zero(t, local.prepared)
	assert.Empty(t, local.lines)
	got := ledger(t, f)
	assert.Len(t, got[string(domain.ModelElevenLabsTTS)], 2)
	assert.NotContains(t, got, string(domain.ModelGPTSoVITSTTS))
}
