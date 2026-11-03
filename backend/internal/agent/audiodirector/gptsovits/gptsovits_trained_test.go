package gptsovits_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/gptsovits"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// fakeServer records the requests of a fake api_v2 behind mam's guard: /mam/voice switches the weights, /tts speaks with whichever is loaded.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	calls    []string // "voice:<id>" and "tts:<voice that was loaded>"
	loaded   string
	refuse   bool
	tokens   []string
	switches int
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tokens = append(f.tokens, r.Header.Get("X-Mam-Token"))
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/mam/voice":
			var in struct{ Voice string }
			_ = json.Unmarshal(raw, &in)
			f.calls = append(f.calls, "voice:"+in.Voice)
			if f.refuse {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"change weights failed: invalid load key"}`))
				return
			}
			f.loaded = in.Voice
			f.switches++
			_, _ = w.Write([]byte(`{"message":"success"}`))
		case "/tts":
			f.calls = append(f.calls, "tts:"+f.loaded)
			_, _ = w.Write(wav)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func speak(t *testing.T, c *gptsovits.Client, trained string) error {
	t.Helper()
	v := voice
	v.Trained = trained
	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "こんにちは"}, v)
	return err
}

func TestClient_ATrainedVoiceIsLoadedOnceAndTheLinesAreSpokenWithIt(t *testing.T) {
	f := newFakeServer(t)
	c := gptsovits.New(&server{url: f.URL, token: "tok-1"}, gptsovits.Config{}, nil)

	require.NoError(t, speak(t, c, "voice-a"))
	require.NoError(t, speak(t, c, "voice-a"))

	assert.Equal(t, []string{"voice:voice-a", "tts:voice-a", "tts:voice-a"}, f.calls, "the weights are loaded once, before the first line")
	assert.Equal(t, []string{"tok-1", "tok-1", "tok-1"}, f.tokens, "the switch carries the token as /tts does")
}

func TestClient_AZeroShotVoiceNeverAsksTheServerToSwitchWhenNothingTrainedIsLoaded(t *testing.T) {
	f := newFakeServer(t)
	c := gptsovits.New(&server{url: f.URL, token: "tok-1"}, gptsovits.Config{}, nil)

	require.NoError(t, speak(t, c, ""))

	assert.Equal(t, []string{"tts:"}, f.calls, "a server that never loaded a trained voice speaks the base one")
}

func TestClient_SwitchingBetweenSpeakersSwitchesTheWeightsAndGoingBackToZeroShotLoadsTheBaseAgain(t *testing.T) {
	f := newFakeServer(t)
	c := gptsovits.New(&server{url: f.URL, token: "tok-1"}, gptsovits.Config{}, nil)

	for _, trained := range []string{"voice-a", "voice-b", "", "", "voice-b"} {
		require.NoError(t, speak(t, c, trained))
	}

	assert.Equal(t, []string{
		"voice:voice-a", "tts:voice-a", "voice:voice-b", "tts:voice-b", "voice:", "tts:", "tts:", "voice:voice-b", "tts:voice-b",
	}, f.calls, "a speaker never gets the voice of the one before")
}

func TestClient_ARestartedServerLosesItsWeightsSoTheVoiceIsLoadedAgain(t *testing.T) {
	f := newFakeServer(t)
	s := &server{url: f.URL, token: "tok-1"}
	c := gptsovits.New(s, gptsovits.Config{}, nil)
	require.NoError(t, speak(t, c, "voice-a"))

	s.token = "tok-2" // the runtime started again: a new token, and the base weights
	f.mu.Lock()
	f.loaded = ""
	f.mu.Unlock()
	require.NoError(t, speak(t, c, "voice-a"))

	assert.Equal(t, []string{"voice:voice-a", "tts:voice-a", "voice:voice-a", "tts:voice-a"}, f.calls)
}

func TestClient_WeightsTheServerCannotLoadStopTheLineInsteadOfSpeakingWithAnotherVoice(t *testing.T) {
	f := newFakeServer(t)
	f.refuse = true
	c := gptsovits.New(&server{url: f.URL, token: "tok-1"}, gptsovits.Config{}, nil)

	err := speak(t, c, "voice-a")

	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Contains(t, un.Detail, "学習した声の重みを読み込めませんでした")
	assert.Contains(t, un.Detail, "invalid load key")
	assert.Equal(t, []string{"voice:voice-a"}, f.calls, "/tts was not called")

	f.mu.Lock()
	f.refuse = false
	f.mu.Unlock()
	require.NoError(t, speak(t, c, "voice-a"), "the next line asks again")
	assert.Equal(t, []string{"voice:voice-a", "voice:voice-a", "tts:voice-a"}, f.calls)
}
