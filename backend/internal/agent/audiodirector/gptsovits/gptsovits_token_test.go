package gptsovits_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/gptsovits"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var dialogue = domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "こんにちは"}

// mam's launcher answers /tts only with the token of this start: the client sends the one the server of the runtime gave.
func TestClient_SendsTheTokenOfTheStartInTheHeaderOfEveryRequest(t *testing.T) {
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("X-Mam-Token"))
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	fake := &server{url: srv.URL, token: "token-of-the-first-start"}
	c := gptsovits.New(fake, gptsovits.Config{}, nil)

	_, err := c.Synthesize(context.Background(), dialogue, voice)
	require.NoError(t, err)
	fake.token = "token-of-the-restart" // the server was restarted: the next request carries its new token
	_, err = c.Synthesize(context.Background(), dialogue, voice)
	require.NoError(t, err)

	assert.Equal(t, []string{"token-of-the-first-start", "token-of-the-restart"}, tokens)
}

// api_v2 is another process with its own working directory (and mam's data directory may be given relative): the recording is sent by
// its absolute path.
func TestClient_SendsTheReferenceRecordingByAnAbsolutePath(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	c := gptsovits.New(&server{url: srv.URL, token: "t"}, gptsovits.Config{}, nil)
	rel := filepath.Join("data", "assets", "p1", "voices", "yui.wav")

	_, err := c.Synthesize(context.Background(), dialogue, localvoice.Voice{Path: rel, Text: "こんにちは", Language: "ja"})

	require.NoError(t, err)
	want, err := filepath.Abs(rel)
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(want))
	assert.Equal(t, want, got["ref_audio_path"])
}
