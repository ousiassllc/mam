package gptsovits_test

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
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/gptsovits"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// wav is the smallest answer api_v2 could give that looks like a wav file.
var wav = append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 24)...)

// server is a Server that answers with the address of a fake api_v2 and the token of its start.
type server struct {
	url   string
	token string
	err   error
	calls atomic.Int32
}

func (s *server) Ensure(context.Context) (string, string, error) {
	s.calls.Add(1)
	return s.url, s.token, s.err
}

var voice = localvoice.Voice{Path: "/assets/p1/voices/yui.wav", Text: "こんにちは、ユイです", Language: "ja"}

func TestClient_PostsTheTextAndTheReferenceVoiceToApiV2AndReturnsTheWav(t *testing.T) {
	var got struct {
		method, path, contentType string
		body                      map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.contentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	fake := &server{url: srv.URL + "/"}
	c := gptsovits.New(fake, gptsovits.Config{}, nil)

	sp, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: " 今日はいい天気ですね "}, voice)

	require.NoError(t, err)
	assert.Equal(t, audiodirector.Speech{Data: wav, Ext: "wav"}, sp)
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "/tts", got.path, "a trailing slash of the base URL does not double")
	assert.Equal(t, "application/json", got.contentType)
	assert.Equal(t, "今日はいい天気ですね", got.body["text"])
	assert.Equal(t, "ja", got.body["text_lang"])
	wantPath, err := filepath.Abs(voice.Path)
	require.NoError(t, err)
	assert.Equal(t, wantPath, got.body["ref_audio_path"])
	assert.Equal(t, "こんにちは、ユイです", got.body["prompt_text"])
	assert.Equal(t, "ja", got.body["prompt_lang"])
	assert.Equal(t, "wav", got.body["media_type"])
	assert.Equal(t, false, got.body["streaming_mode"], "a streamed answer has no header api_v2 could be told apart by")
}

func TestClient_AReferenceVoiceWithoutATranscriptIsSentWithAnEmptyPromptText(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	c := gptsovits.New(&server{url: srv.URL}, gptsovits.Config{}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceNarration, Text: "夕暮れ"}, localvoice.Voice{Path: "/v.wav"})

	require.NoError(t, err)
	assert.Equal(t, "", body["prompt_text"])
	assert.Equal(t, "ja", body["prompt_lang"], "api_v2 requires prompt_lang even without a transcript: the default language is sent")
}

func TestClient_TheLanguageOfTheReferenceVoiceIsThePromptLanguage(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	c := gptsovits.New(&server{url: srv.URL}, gptsovits.Config{TextLanguage: "en"}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "Kai", Text: "Hello"}, localvoice.Voice{Path: "/v.wav", Text: "Hi", Language: "zh"})

	require.NoError(t, err)
	assert.Equal(t, "en", body["text_lang"])
	assert.Equal(t, "zh", body["prompt_lang"])
}

func TestClient_RefusesALineWithoutTextAndAVoiceThatIsNotUsable(t *testing.T) {
	fake := &server{url: "http://127.0.0.1:1"}
	c := gptsovits.New(fake, gptsovits.Config{}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "  "}, voice)
	require.Error(t, err)
	_, err = c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}, localvoice.Voice{})
	require.Error(t, err)

	assert.Zero(t, fake.calls.Load(), "the server is not even asked")
}

func TestClient_Failures(t *testing.T) {
	line := domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "こんにちは"}
	answer := func(status int, body []byte) func(w http.ResponseWriter, r *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write(body)
		}
	}
	for name, tc := range map[string]struct {
		handler func(w http.ResponseWriter, r *http.Request)
		status  int
	}{
		"an HTTP error":               {answer(http.StatusBadRequest, []byte(`{"message":"tts failed","Exception":"ref_audio_path not exists"}`)), http.StatusBadRequest},
		"an empty answer":             {answer(http.StatusOK, nil), http.StatusOK},
		"an answer that is not a wav": {answer(http.StatusOK, []byte(`{"message":"ok"}`)), http.StatusOK},
		"an answer over the limit": {func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(wav)
			chunk := make([]byte, 1<<20)
			for range 33 {
				_, _ = w.Write(chunk)
			}
		}, http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(tc.handler))
			defer srv.Close()
			c := gptsovits.New(&server{url: srv.URL}, gptsovits.Config{}, nil)

			_, err := c.Synthesize(context.Background(), line, voice)

			var pe *audiodirector.ProviderError
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, localvoice.Vendor, pe.Vendor)
			assert.Equal(t, tc.status, pe.Status)
			var un *localvoice.UnavailableError
			assert.False(t, errors.As(err, &un), "a failed line is not an unusable provider")
		})
	}
}

func TestClient_AnHTTPErrorCarriesTheServersMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"tts failed","Exception":"参考音频在3~10秒范围外，请更换！"}`))
	}))
	defer srv.Close()
	c := gptsovits.New(&server{url: srv.URL}, gptsovits.Config{}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}, voice)

	assert.ErrorContains(t, err, "HTTP 400")
	assert.ErrorContains(t, err, "参考音频在3~10秒范围外")
}

// CUDA running out of memory is the one failure of a line that the user can do something about: it is told from the others by kind.
func TestClient_AnOutOfMemoryOfTheGPUIsKindGPUMemory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"tts failed","Exception":"CUDA out of memory. Tried to allocate 2.00 GiB"}`))
	}))
	defer srv.Close()
	c := gptsovits.New(&server{url: srv.URL}, gptsovits.Config{}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}, voice)

	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Equal(t, localvoice.KindGPUMemory, un.Kind)
	assert.Contains(t, un.Detail, "out of memory")
	var pe *audiodirector.ProviderError
	assert.ErrorAs(t, err, &pe, "the HTTP failure is still behind it")
}

func TestClient_ALineThatTakesLongerThanItsLimitIsKindTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // the server notices a gone client only once the request is read
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c := gptsovits.New(&server{url: srv.URL}, gptsovits.Config{Timeout: 50 * time.Millisecond}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}, voice)

	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Equal(t, localvoice.KindTimeout, un.Kind)
}

func TestClient_ACallerThatGivesUpIsNotAFailureOfTheServer(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // the server notices a gone client only once the request is read
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c := gptsovits.New(&server{url: srv.URL}, gptsovits.Config{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-entered
		cancel()
	}()

	_, err := c.Synthesize(ctx, domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}, voice)

	assert.ErrorIs(t, err, context.Canceled)
	var un *localvoice.UnavailableError
	assert.False(t, errors.As(err, &un))
}

func TestClient_AServerThatStoppedAnsweringIsKindProcess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listens any more
	c := gptsovits.New(&server{url: url}, gptsovits.Config{}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}, voice)

	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Equal(t, localvoice.KindProcess, un.Kind)
	assert.NotContains(t, un.Detail, url, "the address of the request is not repeated")
}

func TestClient_TheErrorOfTheServerIsReturnedAsItIs(t *testing.T) {
	want := &localvoice.UnavailableError{Kind: localvoice.KindNotInstalled, Detail: "x"}
	c := gptsovits.New(&server{err: want}, gptsovits.Config{}, nil)

	_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}, voice)
	assert.Same(t, want, err)

	assert.Same(t, want, c.Prepare(context.Background()))
}

func TestClient_PrepareStartsTheServer(t *testing.T) {
	fake := &server{url: "http://127.0.0.1:1"}
	c := gptsovits.New(fake, gptsovits.Config{}, nil)

	require.NoError(t, c.Prepare(context.Background()))

	assert.EqualValues(t, 1, fake.calls.Load())
}

// One GPU: a line does not start while another is being spoken (it waits, and ends with its own context when that is cancelled).
func TestClient_ALineDoesNotStartWhileAnotherIsBeingSpoken(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	c := gptsovits.New(&server{url: srv.URL}, gptsovits.Config{}, nil)
	first := make(chan error, 1)
	go func() {
		_, err := c.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "a"}, voice)
		first <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Synthesize(ctx, domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "ユイ", Text: "b"}, voice)

	assert.ErrorIs(t, err, context.Canceled)
	close(release)
	assert.NoError(t, <-first)
}
