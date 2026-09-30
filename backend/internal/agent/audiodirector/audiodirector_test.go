package audiodirector_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
)

func keys(t *testing.T, withKey bool) credential.Store {
	t.Helper()
	keyring.MockInit()
	store := credential.NewKeyring()
	if withKey {
		require.NoError(t, store.Set(audiodirector.CredentialProvider, "sk-test-key"))
	}
	return store
}

func TestElevenLabs_SendsTheLineWithTheVoiceOfItsKind(t *testing.T) {
	var got struct {
		path, key, query string
		body             map[string]string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.key, got.query = r.URL.Path, r.Header.Get("xi-api-key"), r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		_, _ = w.Write([]byte("ID3-audio"))
	}))
	defer srv.Close()
	cfg := audiodirector.Config{BaseURL: srv.URL, DialogueVoiceID: "voice-d", NarrationVoiceID: "voice-n"}
	tts := audiodirector.NewElevenLabs(cfg, keys(t, true), nil)

	speech, err := tts.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceNarration, Text: "朝が来た"})
	require.NoError(t, err)
	assert.Equal(t, "/v1/text-to-speech/voice-n", got.path)
	assert.Equal(t, "sk-test-key", got.key)
	assert.Equal(t, "朝が来た", got.body["text"])
	assert.Equal(t, "eleven_multilingual_v2", got.body["model_id"])
	assert.Contains(t, got.query, "output_format=mp3")
	assert.Equal(t, []byte("ID3-audio"), speech.Data)
	assert.Equal(t, "mp3", speech.Ext)

	_, err = tts.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "美咲", Text: "こんにちは"})
	require.NoError(t, err)
	assert.Equal(t, "/v1/text-to-speech/voice-d", got.path)
}

func TestElevenLabs_Failures(t *testing.T) {
	t.Run("no API key", func(t *testing.T) {
		tts := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: "http://127.0.0.1:1"}, keys(t, false), nil)

		_, err := tts.Synthesize(context.Background(), domain.VoiceLine{Text: "a"})

		assert.ErrorIs(t, err, audiodirector.ErrNoAPIKey)
	})
	t.Run("HTTP error keeps the status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"detail":"quota exceeded"}`, http.StatusTooManyRequests)
		}))
		defer srv.Close()
		tts := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: srv.URL}, keys(t, true), nil)

		_, err := tts.Synthesize(context.Background(), domain.VoiceLine{Text: "a"})

		var perr *audiodirector.ProviderError
		require.True(t, errors.As(err, &perr))
		assert.Equal(t, http.StatusTooManyRequests, perr.Status)
		assert.Contains(t, perr.Message, "quota exceeded")
	})
	t.Run("the API key is not leaked by a network error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		tts := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: url}, keys(t, true), nil)

		_, err := tts.Synthesize(context.Background(), domain.VoiceLine{Text: "a"})

		require.Error(t, err)
		assert.NotContains(t, err.Error(), "sk-test-key")
	})
}

// fakeTTS answers every line with a tone of one second, and remembers the lines.
type fakeTTS struct {
	t     *testing.T
	tools string
	lines []domain.VoiceLine
}

func (f *fakeTTS) Synthesize(ctx context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	f.lines = append(f.lines, line)
	out := filepath.Join(f.t.TempDir(), "tone.mp3")
	cmd := exec.CommandContext(ctx, f.tools, "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=300:duration=1", "-af", "volume=0.3", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("make tone: %v: %s", err, b)
	}
	data, err := os.ReadFile(out)
	return audiodirector.Speech{Data: data, Ext: "mp3"}, err
}

func TestDirector_PlacesLinesAfterEachOtherAndNormalizesTheMix(t *testing.T) {
	tools, err := media.Look()
	if err != nil {
		t.Skip("ffmpeg / ffprobe are not installed")
	}
	root := t.TempDir()
	tts := &fakeTTS{t: t, tools: "ffmpeg"}
	d := audiodirector.New(tts, tools, root)

	got, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Seconds: 6,
		Cues: []audiodirector.Cue{
			{Start: 0.5, Lines: []string{"美咲: こんにちは", "ナレーション: 朝が来た"}},
			{Start: 1.0, Lines: []string{"健: おはよう"}}, // its Scene starts before the previous lines end
		},
	})

	require.NoError(t, err)
	require.Len(t, got.Clips, 3)
	assert.Equal(t, []domain.VoiceKind{domain.VoiceDialogue, domain.VoiceNarration, domain.VoiceDialogue}, []domain.VoiceKind{got.Clips[0].Kind, got.Clips[1].Kind, got.Clips[2].Kind})
	assert.InDelta(t, 0.5, got.Clips[0].StartSeconds, 1e-9)
	assert.Greater(t, got.Clips[1].StartSeconds, got.Clips[0].End(), "lines do not overlap")
	assert.Greater(t, got.Clips[2].StartSeconds, got.Clips[1].End(), "the next Scene waits for the speech to end")
	assert.True(t, got.SpeechExpected)
	assert.Equal(t, "p1/audio/e1/mix.m4a", got.MixPath)
	assert.InDelta(t, domain.DefaultTargetLUFS, got.MeasuredLUFS, 1.0)
	_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(got.MixPath)))
	assert.NoError(t, statErr)
	assert.Equal(t, "こんにちは", tts.lines[0].Text)
}

func TestDirector_NoLinesAndNoSoundMakesNoMix(t *testing.T) {
	tools, err := media.Look()
	if err != nil {
		t.Skip("ffmpeg / ffprobe are not installed")
	}
	d := audiodirector.New(nil, tools, t.TempDir())

	got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Seconds: 3})

	require.NoError(t, err)
	assert.False(t, got.SpeechExpected)
	assert.Empty(t, got.MixPath)
	assert.Empty(t, got.Clips)
}

func TestDirector_LinesWithoutASynthesizerFail(t *testing.T) {
	tools, err := media.Look()
	if err != nil {
		t.Skip("ffmpeg / ffprobe are not installed")
	}
	d := audiodirector.New(nil, tools, t.TempDir())

	_, err = d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Seconds: 3, Cues: []audiodirector.Cue{{Lines: []string{"a: b"}}}})

	assert.ErrorIs(t, err, audiodirector.ErrNoAPIKey)
}
