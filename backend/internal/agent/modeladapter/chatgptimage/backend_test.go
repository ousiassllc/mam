package chatgptimage_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptimage"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

const token = "SECRET-CODEX-ACCESS"

type source struct {
	err   error
	calls atomic.Int32
}

func (s *source) ImageCredentials(context.Context) (chatgpt.Credentials, error) {
	s.calls.Add(1)
	if s.err != nil {
		return chatgpt.Credentials{}, s.err
	}
	return chatgpt.Credentials{AccessToken: token, AccountID: "acct-7"}, nil
}

type codex struct {
	*httptest.Server
	mu      sync.Mutex
	body    map[string]any
	headers http.Header
	n       atomic.Int32 // the calls of /responses (or any other path but /models)
	// listed counts the reads of /models; models answers them (nil: 404, as a Codex backend without a list does).
	listed atomic.Int32
	models func(w http.ResponseWriter, r *http.Request)
}

func (c *codex) setModels(models func(w http.ResponseWriter, r *http.Request)) {
	c.mu.Lock()
	c.models = models
	c.mu.Unlock()
}

func (c *codex) serveModels(w http.ResponseWriter, r *http.Request) {
	c.listed.Add(1)
	c.mu.Lock()
	models := c.models
	c.mu.Unlock()
	if models == nil {
		http.NotFound(w, r)
		return
	}
	models(w, r)
}

// newCodexWith is a Codex backend whose every path but /models is answered by serve.
func newCodexWith(t *testing.T, serve func(c *codex, w http.ResponseWriter, r *http.Request)) *codex {
	t.Helper()
	c := &codex{}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			c.serveModels(w, r)
			return
		}
		serve(c, w, r)
	}))
	t.Cleanup(c.Close)
	return c
}

func newCodex(t *testing.T, reply func(w http.ResponseWriter, f http.Flusher)) *codex {
	t.Helper()
	return newCodexWith(t, func(c *codex, w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		_ = json.Unmarshal(raw, &c.body)
		c.headers = r.Header.Clone()
		c.mu.Unlock()
		c.n.Add(1)
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		reply(w, w.(http.Flusher))
	})
}

func data(w http.ResponseWriter, events ...string) {
	for _, e := range events {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
	}
}

var png = []byte("\x89PNG-image-bytes")

func imageDone(format string) string {
	return fmt.Sprintf(`{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"completed","output_format":%q,"result":%q}}`,
		format, base64.StdEncoding.EncodeToString(png))
}

func generate(c *codex, src chatgptimage.CredentialSource, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	b := chatgptimage.New(src, chatgptimage.Options{BaseURL: c.URL, Stall: 300 * time.Millisecond})
	return b.GenerateImage(context.Background(), "gpt-image-2", req)
}

func TestGenerateImage_CallsTheCodexResponsesEndpointWithTheImageTool(t *testing.T) {
	c := newCodex(t, func(w http.ResponseWriter, _ http.Flusher) {
		data(w, `{"type":"response.created"}`, imageDone("png"), `{"type":"response.completed"}`)
	})
	res, err := generate(c, &source{}, modeladapter.ImageRequest{
		Prompt: "a red fox", AspectRatio: "16:9",
		Inputs: []modeladapter.Input{{Data: []byte{9, 9}, MimeType: "image/jpeg"}},
	})
	require.NoError(t, err)
	assert.Equal(t, png, res.Data)
	assert.Equal(t, "image/png", res.MimeType)
	require.NotNil(t, res.ActualCost)
	assert.Zero(t, *res.ActualCost, "the plan pays: 0 USD")

	assert.Equal(t, "Bearer "+token, c.headers.Get("Authorization"))
	assert.Equal(t, "acct-7", c.headers.Get("ChatGPT-Account-Id"))
	assert.Equal(t, "responses=experimental", c.headers.Get("OpenAI-Beta"))
	assert.Equal(t, "codex_cli_rs", c.headers.Get("originator"))
	assert.NotEmpty(t, c.headers.Get("session_id"))
	assert.Equal(t, "text/event-stream", c.headers.Get("Accept"))

	assert.Equal(t, true, c.body["stream"])
	assert.Equal(t, false, c.body["store"])
	assert.Equal(t, chatgptimage.FallbackDrivers[0], c.body["model"], "no model list: the first fallback driver hosts the call")
	assert.NotEmpty(t, c.body["instructions"], "the endpoint needs instructions to call the tool")
	assert.Equal(t, map[string]any{"type": "image_generation"}, c.body["tool_choice"])
	tool := c.body["tools"].([]any)[0].(map[string]any)
	assert.Equal(t, "image_generation", tool["type"])
	assert.Equal(t, "gpt-image-2", tool["model"], "the image model travels inside the tool")
	assert.Equal(t, "1536x1024", tool["size"])
	parts := c.body["input"].([]any)[0].(map[string]any)["content"].([]any)
	require.Len(t, parts, 2)
	assert.Equal(t, "a red fox", parts[0].(map[string]any)["text"])
	assert.Equal(t, "input_image", parts[1].(map[string]any)["type"], "reference images are input_image")
	assert.Equal(t, "data:image/jpeg;base64,CQk=", parts[1].(map[string]any)["image_url"])
}

func TestGenerateImage_FailuresAreErrors(t *testing.T) {
	cases := []struct {
		name   string
		reply  func(w http.ResponseWriter, f http.Flusher)
		status int
		msg    string
	}{
		{"failed", func(w http.ResponseWriter, _ http.Flusher) {
			data(w, `{"type":"response.failed","response":{"error":{"message":"policy"}}}`)
		}, 400, "policy"},
		{"moderation stop is incomplete", func(w http.ResponseWriter, _ http.Flusher) {
			data(w, `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"content_filter"}}}`)
		}, 400, "content_filter"},
		{"error event", func(w http.ResponseWriter, _ http.Flusher) { data(w, `{"type":"error","error":{"message":"nope"}}`) }, 400, "nope"},
		{"no image", func(w http.ResponseWriter, _ http.Flusher) { data(w, `{"type":"response.completed"}`) }, 200, "no image"},
		{"bad base64", func(w http.ResponseWriter, _ http.Flusher) {
			data(w, `{"type":"response.output_item.done","item":{"type":"image_generation_call","result":"!!!"}}`)
		}, 200, "base64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCodex(t, tc.reply)
			_, err := generate(c, &source{}, modeladapter.ImageRequest{Prompt: "x"})
			var pe *modeladapter.ProviderError
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, tc.status, pe.Status)
			assert.Contains(t, pe.Message, tc.msg)
			assert.NotContains(t, err.Error(), token)
		})
	}
}

func TestGenerateImage_HTTPErrorsAndCloudflare(t *testing.T) {
	c := newCodex(t, func(http.ResponseWriter, http.Flusher) {})
	c.Close()
	_, err := generate(c, &source{}, modeladapter.ImageRequest{Prompt: "x"})
	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Zero(t, pe.Status)
	assert.NotContains(t, err.Error(), c.URL)

	for status, body := range map[int]string{429: `{"error":{"message":"slow down"}}`, 403: `<html>Cloudflare challenge</html>`, 500: `{"detail":"broken"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		_, err := chatgptimage.New(&source{}, chatgptimage.Options{BaseURL: srv.URL}).GenerateImage(context.Background(), "m", modeladapter.ImageRequest{Prompt: "x"})
		srv.Close()
		require.ErrorAs(t, err, &pe, status)
		assert.Equal(t, status, pe.Status)
		assert.Equal(t, 3*time.Second, pe.RetryAfter)
		if status == 403 {
			assert.Contains(t, pe.Message, "Cloudflare")
		}
	}
}

func TestGenerateImage_AStreamThatGoesSilentIsGivenUp(t *testing.T) {
	c := newCodex(t, func(w http.ResponseWriter, f http.Flusher) {
		data(w, `{"type":"response.created"}`)
		f.Flush()
		time.Sleep(time.Second) // longer than the 300 ms stall time of the test
	})
	start := time.Now()
	_, err := generate(c, &source{}, modeladapter.ImageRequest{Prompt: "x"})
	require.ErrorIs(t, err, chatgptimage.ErrStalled)
	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Zero(t, pe.Status, "a stall is retried like a network failure")
	assert.Less(t, time.Since(start), 950*time.Millisecond)
}

func TestGenerateImage_OffMeansNoRequest(t *testing.T) {
	c := newCodex(t, func(http.ResponseWriter, http.Flusher) { t.Error("no request expected") })
	src := &source{err: chatgpt.ErrImageDisabled}
	_, err := generate(c, src, modeladapter.ImageRequest{Prompt: "x"})
	require.ErrorIs(t, err, chatgpt.ErrImageDisabled)
	assert.Zero(t, c.n.Load(), "OFF: zero outbound calls")
	var pe *modeladapter.ProviderError
	assert.False(t, errors.As(err, &pe), "a switch that is off is not a vendor failure: it is not retried or counted by the breaker")

	src = &source{err: chatgpt.ErrNotSignedIn}
	_, err = generate(c, src, modeladapter.ImageRequest{Prompt: "x"})
	require.ErrorIs(t, err, chatgpt.ErrNotSignedIn)
	assert.Zero(t, c.n.Load())
}
