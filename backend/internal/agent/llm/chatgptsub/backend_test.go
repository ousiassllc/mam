package chatgptsub_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/chatgptsub"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

const secretToken = "SECRET-ACCESS-TOKEN-VALUE"

// tokens is a TokenSource with a scripted outcome.
type tokens struct {
	err        error
	refreshes  atomic.Int32
	refreshErr error
}

func (t *tokens) Credentials(context.Context) (chatgpt.Credentials, error) {
	if t.err != nil {
		return chatgpt.Credentials{}, t.err
	}
	return chatgpt.Credentials{AccessToken: secretToken}, nil
}

func (t *tokens) Refresh(context.Context) (chatgpt.Credentials, error) {
	t.refreshes.Add(1)
	return chatgpt.Credentials{AccessToken: secretToken + "-2"}, t.refreshErr
}

// responses is a fake Responses API. Each request is recorded; reply writes the answer.
type responses struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []map[string]any
	auth []string
	n    atomic.Int32
}

func newResponses(t *testing.T, reply func(w http.ResponseWriter, n int)) *responses {
	t.Helper()
	r := &responses{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		r.mu.Lock()
		r.reqs = append(r.reqs, body)
		r.auth = append(r.auth, req.Header.Get("Authorization"))
		r.mu.Unlock()
		if req.URL.Path != "/v1/responses" {
			http.NotFound(w, req)
			return
		}
		reply(w, int(r.n.Add(1)))
	}))
	t.Cleanup(r.Close)
	return r
}

func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		_, _ = fmt.Fprintf(w, "event: x\ndata: %s\n\n", e)
	}
}

func complete(srv *responses, src chatgptsub.TokenSource, req llm.Request) (string, error) {
	return chatgptsub.New(srv.URL, src, nil).Complete(context.Background(), "some-model", req)
}

func TestComplete_SendsTheOfficialPreviewShapeAndReturnsTheStreamedText(t *testing.T) {
	srv := newResponses(t, func(w http.ResponseWriter, _ int) {
		sse(w, `{"type":"response.created"}`, `{"type":"response.output_text.delta","delta":"Hel"}`,
			`{"type":"response.output_text.delta","delta":"lo"}`, `{"type":"response.output_text.done","text":"Hello"}`,
			`{"type":"response.completed","response":{"output":[]}}`)
	})
	got, err := complete(srv, &tokens{}, llm.Request{
		System: "be brief", User: "hi", MaxTokens: 8192, JSON: true,
		Images: []llm.Image{{MimeType: "image/png", Data: []byte{1, 2, 3}, Caption: "frame 1"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Hello", got)

	body := srv.reqs[0]
	assert.Equal(t, "some-model", body["model"])
	assert.Equal(t, false, body["store"])
	assert.Equal(t, true, body["stream"])
	assert.Contains(t, body["instructions"], "be brief")
	assert.Contains(t, body["instructions"], "JSON object", "JSON is demanded in words: text.format is unverified")
	for _, unsupported := range []string{"max_output_tokens", "temperature", "top_p", "metadata", "user", "text", "truncation", "background"} {
		assert.NotContains(t, body, unsupported, "the preview does not support %s", unsupported)
	}
	input, ok := body["input"].([]any)
	require.True(t, ok, "input is an array")
	require.Len(t, input, 1)
	msg := input[0].(map[string]any)
	assert.Equal(t, "user", msg["role"], "no role system item")
	parts := msg["content"].([]any)
	require.Len(t, parts, 3)
	assert.Equal(t, "input_text", parts[0].(map[string]any)["type"])
	assert.Equal(t, "frame 1", parts[1].(map[string]any)["text"])
	assert.Equal(t, "data:image/png;base64,AQID", parts[2].(map[string]any)["image_url"])
	assert.Equal(t, "Bearer "+secretToken, srv.auth[0])
}

func TestComplete_UsesTheCompletedResponseWhenThereAreNoDeltas(t *testing.T) {
	srv := newResponses(t, func(w http.ResponseWriter, _ int) {
		sse(w, `{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"from the object"}]}]}}`)
	})
	got, err := complete(srv, &tokens{}, llm.Request{User: "x"})
	require.NoError(t, err)
	assert.Equal(t, "from the object", got)
	assert.Contains(t, srv.reqs[0]["instructions"], "assistant", "instructions are always sent")
}

func TestComplete_OnlyCompletedIsSuccess(t *testing.T) {
	cases := []struct {
		name        string
		events      []string
		wantStatus  int
		wantPerm    bool
		wantNotice  string
		wantMessage string
	}{
		{"failed with the usage limit", []string{`{"type":"response.output_text.delta","delta":"par"}`, `{"type":"response.failed","response":{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"limit"}}}`}, 429, true, "https://chatgpt.com/settings/usage", "subscription_sharing_usage_limit_exceeded"},
		{"failed with usage unavailable retries", []string{`{"type":"response.failed","response":{"error":{"code":"subscription_sharing_usage_unavailable"}}}`}, 503, false, "", ""},
		{"failed with a server error retries", []string{`{"type":"response.failed","response":{"error":{"code":"server_error","message":"boom"}}}`}, 503, false, "", "boom"},
		{"failed with an unknown code does not retry", []string{`{"type":"response.failed","response":{"error":{"code":"something_new"}}}`}, 422, true, "", "something_new"},
		{"incomplete", []string{`{"type":"response.output_text.delta","delta":"cut"}`, `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`}, 200, true, "", "max_output_tokens"},
		{"error event", []string{`{"type":"error","code":"subscription_sharing_user_not_eligible","message":"no"}`}, 403, true, "チャット", ""},
		{"completed with no text", []string{`{"type":"response.completed","response":{"output":[]}}`}, 200, false, "", "no text"},
		{"dropped stream", []string{`{"type":"response.output_text.delta","delta":"half"}`}, 0, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newResponses(t, func(w http.ResponseWriter, _ int) { sse(w, tc.events...) })
			got, err := complete(srv, &tokens{}, llm.Request{User: "x"})
			require.Error(t, err)
			assert.Empty(t, got, "a partial text is never returned")
			var api *llm.APIError
			require.ErrorAs(t, err, &api)
			assert.Equal(t, tc.wantStatus, api.Status)
			assert.Equal(t, tc.wantPerm, api.Permanent)
			if tc.wantNotice != "" && tc.wantNotice != "チャット" {
				assert.Contains(t, api.Notice, tc.wantNotice)
			}
			assert.Contains(t, api.Message, tc.wantMessage)
			assert.NotContains(t, err.Error(), secretToken)
		})
	}
}

func TestComplete_HTTPErrorsFollowTheTable(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantPerm   bool
	}{
		{"usage limit", 429, `{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"x"}}`, 429, true},
		{"plain rate limit retries", 429, `{"error":{"code":"rate_limit_exceeded"}}`, 429, false},
		{"not eligible", 403, `{"error":{"code":"subscription_sharing_user_not_eligible"}}`, 403, true},
		{"usage unavailable", 503, `{"error":{"code":"subscription_sharing_usage_unavailable"}}`, 503, false},
		{"routing unavailable detail", 503, `{"detail":"direct routing unavailable"}`, 503, false},
		{"unsupported capability", 400, `{"error":{"code":"subscription_sharing_unsupported_capability","param":"tools"}}`, 400, true},
		{"route not supported", 403, `{"error":{"code":"subscription_sharing_route_not_supported"}}`, 403, true},
		{"scope not authorized", 403, `{"error":{"code":"chatpass_v2_scope_not_authorized"}}`, 403, true},
		{"unknown 4xx", 418, `{"error":{"code":"teapot"}}`, 418, false},
		{"unknown 5xx", 502, `bad gateway`, 502, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newResponses(t, func(w http.ResponseWriter, _ int) {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := complete(srv, &tokens{}, llm.Request{User: "x"})
			var api *llm.APIError
			require.ErrorAs(t, err, &api)
			assert.Equal(t, tc.wantStatus, api.Status)
			assert.Equal(t, tc.wantPerm, api.Permanent)
			assert.Equal(t, 7*time.Second, api.RetryAfter)
		})
	}
}

func TestComplete_A401IsConfirmedByARefreshAndThenRetriedOnce(t *testing.T) {
	srv := newResponses(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			w.WriteHeader(401)
			return
		}
		sse(w, `{"type":"response.output_text.delta","delta":"ok"}`, `{"type":"response.completed"}`)
	})
	src := &tokens{}
	got, err := complete(srv, src, llm.Request{User: "x"})
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
	assert.EqualValues(t, 1, src.refreshes.Load())
	assert.Equal(t, []string{"Bearer " + secretToken, "Bearer " + secretToken}, srv.auth, "the second try uses the credentials the account holds then")

	always := newResponses(t, func(w http.ResponseWriter, _ int) { w.WriteHeader(401) })
	_, err = complete(always, &tokens{}, llm.Request{User: "x"})
	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, 401, api.Status)
	assert.EqualValues(t, 2, always.n.Load(), "one retry, not a loop")

	src = &tokens{refreshErr: chatgpt.ErrReloginRequired}
	_, err = complete(newResponses(t, func(w http.ResponseWriter, _ int) { w.WriteHeader(401) }), src, llm.Request{User: "x"})
	require.ErrorAs(t, err, &api)
	assert.True(t, api.Permanent)
	assert.Contains(t, api.Notice, "ログインし直して")
}

func TestComplete_SignInProblemsAreNoticesNotRetriesAndMakeNoCall(t *testing.T) {
	for _, tc := range []struct {
		err    error
		notice string
	}{
		{chatgpt.ErrNotSignedIn, "ログインしていません"},
		{chatgpt.ErrReloginRequired, "有効期限"},
		{chatgpt.ErrPlanUsageNotGranted, "許可"},
	} {
		srv := newResponses(t, func(http.ResponseWriter, int) { t.Error("no request expected") })
		_, err := complete(srv, &tokens{err: tc.err}, llm.Request{User: "x"})
		var api *llm.APIError
		require.ErrorAs(t, err, &api)
		assert.True(t, api.Permanent)
		assert.Contains(t, api.Notice, tc.notice)
		require.ErrorIs(t, err, tc.err)
	}
	_, err := complete(newResponses(t, func(http.ResponseWriter, int) {}), &tokens{err: errors.New("keyring is locked")}, llm.Request{User: "x"})
	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.False(t, api.Permanent, "a store that does not answer may answer later")
}

func TestComplete_ANetworkFailureHasNoURL(t *testing.T) {
	srv := newResponses(t, func(http.ResponseWriter, int) {})
	b := chatgptsub.New(srv.URL, &tokens{}, nil)
	srv.Close()
	_, err := b.Complete(context.Background(), "m", llm.Request{User: "x"})
	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.Zero(t, api.Status)
	assert.NotContains(t, err.Error(), srv.URL)
	assert.False(t, strings.Contains(err.Error(), secretToken))
}

func errNotSignedIn() error { return chatgpt.ErrNotSignedIn }
