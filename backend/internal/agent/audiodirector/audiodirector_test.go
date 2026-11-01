package audiodirector_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/synclabs"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
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
			http.Error(w, "quota\n\n  exceeded "+strings.Repeat("あ", 400), http.StatusTooManyRequests)
		}))
		defer srv.Close()
		tts := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: srv.URL}, keys(t, true), nil)
		_, err := tts.Synthesize(context.Background(), domain.VoiceLine{Text: "a"})

		var perr *audiodirector.ProviderError
		require.True(t, errors.As(err, &perr))
		assert.Equal(t, http.StatusTooManyRequests, perr.Status)
		assert.Regexp(t, "^quota exceeded あ+…$", perr.Message, "the body is collapsed to one line and cut on a rune boundary")
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

func TestElevenLabs_ASpeakerWithAClonedVoiceIsSpokenByIt(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte("ID3"))
	}))
	defer srv.Close()
	tts := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: srv.URL, DialogueVoiceID: "voice-d"}, keys(t, true), nil)

	_, err := tts.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "美咲", Text: "a", VoiceID: "cloned-1"})

	require.NoError(t, err)
	assert.Equal(t, "/v1/text-to-speech/cloned-1", path)
}

func TestElevenLabs_MusicAndSoundEffectsSendTheirRequests(t *testing.T) {
	type call struct {
		path, key string
		body      map[string]any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := call{path: r.URL.Path, key: r.Header.Get("xi-api-key")}
		_ = json.Unmarshal(raw, &c.body)
		calls = append(calls, c)
		_, _ = w.Write([]byte("ID3"))
	}))
	defer srv.Close()
	el := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: srv.URL}, keys(t, true), nil)

	music, err := el.Compose(context.Background(), "calm piano", 12.5)
	require.NoError(t, err)
	sfx, err := el.Generate(context.Background(), "door slam", 1.5, false)
	require.NoError(t, err)
	_, err = el.Generate(context.Background(), "rain", 20, true)
	require.NoError(t, err)

	require.Len(t, calls, 3)
	assert.Equal(t, "/v1/music", calls[0].path)
	assert.Equal(t, "sk-test-key", calls[0].key)
	assert.Equal(t, "calm piano", calls[0].body["prompt"])
	assert.EqualValues(t, 12500, calls[0].body["music_length_ms"])
	assert.Equal(t, "music_v2", calls[0].body["model_id"])
	assert.Equal(t, true, calls[0].body["force_instrumental"])
	assert.Equal(t, "/v1/sound-generation", calls[1].path)
	assert.Equal(t, "door slam", calls[1].body["text"])
	assert.EqualValues(t, 1.5, calls[1].body["duration_seconds"])
	assert.Equal(t, false, calls[1].body["loop"])
	assert.Equal(t, true, calls[2].body["loop"])
	assert.Equal(t, "mp3", music.Ext)
	assert.Equal(t, []byte("ID3"), sfx.Data)
}

func TestElevenLabs_SoundLengthsOutsideTheApiBoundsAreRefusedBeforeTheCall(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	el := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: srv.URL}, keys(t, true), nil)

	_, err1 := el.Compose(context.Background(), "x", 2)
	_, err2 := el.Compose(context.Background(), "x", 601)
	_, err3 := el.Generate(context.Background(), "x", 0.2, false)
	_, err4 := el.Generate(context.Background(), "x", 31, false)

	for _, err := range []error{err1, err2, err3, err4} {
		assert.Error(t, err)
	}
	assert.Zero(t, calls)
}

func TestElevenLabs_CloneUploadsTheSampleAndReturnsTheVoiceID(t *testing.T) {
	var name, filename, ctype string
	var sample []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctype = r.Header.Get("Content-Type")
		require.Equal(t, "/v1/voices/add", r.URL.Path)
		require.Equal(t, "sk-test-key", r.Header.Get("xi-api-key"))
		_, params, _ := mime.ParseMediaType(ctype)
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "name":
				name = string(data)
			case "files":
				filename, sample = part.FileName(), data
			}
		}
		_, _ = w.Write([]byte(`{"voice_id":"voice-42","requires_verification":false}`))
	}))
	defer srv.Close()
	el := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: srv.URL}, keys(t, true), nil)

	id, err := el.Clone(context.Background(), "美咲", audiodirector.Sample{Filename: "misaki.mp3", Data: []byte("sample-bytes")})

	require.NoError(t, err)
	assert.Equal(t, "voice-42", id)
	assert.Equal(t, "美咲", name)
	assert.Equal(t, "misaki.mp3", filename)
	assert.Equal(t, []byte("sample-bytes"), sample)
	assert.Contains(t, ctype, "multipart/form-data")
}

func TestElevenLabs_CloneWithoutAVoiceIDInTheAnswerFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()
	el := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: srv.URL}, keys(t, true), nil)

	_, err := el.Clone(context.Background(), "x", audiodirector.Sample{Data: []byte("a")})

	var perr *audiodirector.ProviderError
	assert.True(t, errors.As(err, &perr))
}

func syncKeys(t *testing.T, withKey bool) credential.Store {
	t.Helper()
	keyring.MockInit()
	store := credential.NewKeyring()
	if withKey {
		require.NoError(t, store.Set(audiodirector.SyncCredentialProvider, "sync-test-key"))
	}
	return store
}

func TestDefaultClientsDoNotForwardTheKeyOnARedirectToAnotherHost(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignHits.Add(1) }))
	defer foreign.Close()
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/steal", http.StatusFound)
	}))
	defer vendor.Close()

	tts := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: vendor.URL}, keys(t, true), nil)
	_, err := tts.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceNarration, Text: "a"})
	require.Error(t, err)

	sl := synclabs.New(synclabs.Config{BaseURL: vendor.URL}, syncKeys(t, true), nil)
	_, err = sl.Sync(context.Background(), audiodirector.LipSyncRequest{
		Model: "sync-3", Video: audiodirector.Sample{Filename: "v.mp4", Data: []byte("v")}, Audio: audiodirector.Sample{Filename: "a.wav", Data: []byte("a")},
	})
	require.Error(t, err)

	assert.Zero(t, foreignHits.Load())
}

// Sync Labs bills a generation it accepted: a failure after the job exists is a ChargedError, a
