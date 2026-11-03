package irodori_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/irodori"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// wav is the smallest answer the server could give that looks like a wav file.
var wav = append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 24)...)

// server is a localvoice.Server that answers with the address of a fake Irodori-TTS server and the token of its start.
type server struct {
	url, token string
	err        error
	calls      atomic.Int32
}

func (s *server) Ensure(context.Context) (string, string, error) {
	s.calls.Add(1)
	return s.url, s.token, s.err
}

func voiceOf(t *testing.T) localvoice.Voice {
	t.Helper()
	return localvoice.Voice{Path: writeFile(t, "yui.wav", wavHeader(8)), Profile: localvoice.Profile{Voice: "若い女性の声", Personality: "控えめ"}}
}

var line = domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: " そうなんだ（笑） "}

func TestClient_PostsTheLineTheReferenceAndTheCaptionAndReturnsTheWav(t *testing.T) {
	var got struct {
		method, path, token, contentType string
		body                             map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.token, got.contentType = r.Method, r.URL.Path, r.Header.Get("X-Mam-Token"), r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	voice := voiceOf(t)
	c := irodori.New(&server{url: srv.URL + "/", token: "tok-1"}, irodori.Config{}, nil)
	ctx := localvoice.WithEmotion(context.Background(), "緊張")

	sp, err := c.Synthesize(ctx, line, voice)

	require.NoError(t, err)
	assert.Equal(t, audiodirector.Speech{Data: wav, Ext: "wav"}, sp)
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "/v1/audio/speech", got.path, "a trailing slash of the base URL does not double")
	assert.Equal(t, "tok-1", got.token)
	assert.Equal(t, "application/json", got.contentType)
	assert.Equal(t, "irodori-tts", got.body["model"])
	assert.Equal(t, "そうなんだ🤭", got.body["input"], "the stage direction became its emoji")
	assert.Equal(t, "wav", got.body["response_format"])
	opts, ok := got.body["irodori"].(map[string]any)
	require.True(t, ok)
	wantRef, err := filepath.Abs(voice.Path)
	require.NoError(t, err)
	assert.Equal(t, wantRef, opts["ref_wav"])
	assert.Equal(t, "性格は控えめ。緊張して、声が少し張り詰めた話し方。", opts["caption"], "a voice cloned from a recording leaves the note on the voice out")
}

func TestClient_ALineWithoutTextIsRefusedBeforeTheServerIsAsked(t *testing.T) {
	fake := &server{}
	c := irodori.New(fake, irodori.Config{}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "  "}, voiceOf(t))

	require.Error(t, err)
	assert.Zero(t, fake.calls.Load())
}

func TestClient_AReferenceVoiceIrodoriCannotBeGivenIsRefusedBeforeTheServerIsStarted(t *testing.T) {
	fake := &server{}
	c := irodori.New(fake, irodori.Config{}, nil)

	_, err := c.Synthesize(context.Background(), line, localvoice.Voice{Path: writeFile(t, "empty.wav", nil)})

	var re *localvoice.ReferenceError
	require.True(t, errors.As(err, &re))
	assert.Equal(t, "ユイ", re.Speaker)
	assert.Zero(t, fake.calls.Load(), "the server is not started for a voice it cannot be given")
}

func TestClient_ServerAnswersBecomeTypedErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status   int
		body     string
		wantKind localvoice.Kind
		provider bool
	}{
		"out of memory is the GPU memory":  {500, `{"error":{"message":"CUDA out of memory. Tried to allocate 2.00 GiB","type":"server_error"}}`, localvoice.KindGPUMemory, false},
		"a busy model is a timeout":        {503, `{"error":{"message":"model is loading","type":"server_error"}}`, localvoice.KindTimeout, false},
		"any other answer is the vendor's": {500, `{"error":{"message":"decoder failed"}}`, "", true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := irodori.New(&server{url: srv.URL}, irodori.Config{}, nil)

			_, err := c.Synthesize(context.Background(), line, voiceOf(t))

			if tc.provider {
				var pe *audiodirector.ProviderError
				require.True(t, errors.As(err, &pe))
				assert.Equal(t, localvoice.VendorIrodori, pe.Vendor)
				assert.Equal(t, tc.status, pe.Status)
				assert.Contains(t, pe.Message, "decoder failed")
				return
			}
			var ue *localvoice.UnavailableError
			require.True(t, errors.As(err, &ue))
			assert.Equal(t, tc.wantKind, ue.Kind)
			assert.Equal(t, localvoice.VendorIrodori, ue.Vendor)
		})
	}
}

func TestClient_ANonWavAnswerIsAFaultOfTheVendor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not audio")) }))
	defer srv.Close()
	c := irodori.New(&server{url: srv.URL}, irodori.Config{}, nil)

	_, err := c.Synthesize(context.Background(), line, voiceOf(t))

	var pe *audiodirector.ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, localvoice.VendorIrodori, pe.Vendor)
}

func TestClient_AServerThatDoesNotAnswerInTimeIsATimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c := irodori.New(&server{url: srv.URL}, irodori.Config{Timeout: 50 * time.Millisecond}, nil)

	_, err := c.Synthesize(context.Background(), line, voiceOf(t))

	var ue *localvoice.UnavailableError
	require.True(t, errors.As(err, &ue))
	assert.Equal(t, localvoice.KindTimeout, ue.Kind)
}

func TestClient_AServerThatIsGoneIsTheProcessBeingDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := irodori.New(&server{url: url}, irodori.Config{}, nil)

	_, err := c.Synthesize(context.Background(), line, voiceOf(t))

	var ue *localvoice.UnavailableError
	require.True(t, errors.As(err, &ue))
	assert.Equal(t, localvoice.KindProcess, ue.Kind)
}

func TestClient_ACancelledCallStaysACancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := irodori.New(&server{url: "http://127.0.0.1:1"}, irodori.Config{}, nil)

	_, err := c.Synthesize(ctx, line, voiceOf(t))

	assert.ErrorIs(t, err, context.Canceled)
}

func TestClient_PrepareStartsTheServerAndReturnsItsError(t *testing.T) {
	boom := &localvoice.UnavailableError{Kind: localvoice.KindProcess, Vendor: localvoice.VendorIrodori}
	fake := &server{err: boom}

	err := irodori.New(fake, irodori.Config{}, nil).Prepare(context.Background())

	assert.ErrorIs(t, err, boom)
	assert.EqualValues(t, 1, fake.calls.Load())
}

func TestClient_IsAVoiceCheckerThatRefusesBadReferences(t *testing.T) {
	c := irodori.New(&server{}, irodori.Config{}, nil)
	var checker localvoice.VoiceChecker = c

	assert.NoError(t, checker.CheckVoices(map[string]localvoice.Voice{"ユイ": voiceOf(t)}))
	assert.Error(t, checker.CheckVoices(map[string]localvoice.Voice{"ユイ": {Path: writeFile(t, "e.wav", nil)}}))
}
