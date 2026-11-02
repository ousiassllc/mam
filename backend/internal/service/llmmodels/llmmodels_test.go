package llmmodels_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

const secret = "sk-secret-value"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// vendor answers the model list of OpenAI and counts the asks.
type vendor struct {
	*httptest.Server
	asks   atomic.Int32
	status atomic.Int32
}

func newVendor(t *testing.T) *vendor {
	t.Helper()
	v := &vendor{}
	v.status.Store(200)
	v.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		v.asks.Add(1)
		w.WriteHeader(int(v.status.Load()))
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-live-1","created":2},{"id":"gpt-live-2","created":1}]}`))
	}))
	t.Cleanup(v.Close)
	return v
}

func keys(t *testing.T, pairs ...string) credential.Store {
	t.Helper()
	store := credentialtest.New()
	for i := 0; i < len(pairs); i += 2 {
		require.NoError(t, store.Set(pairs[i], pairs[i+1]))
	}
	return store
}

func ids(l llmmodels.List) []string {
	var out []string
	for _, m := range l.Models {
		out = append(out, m.ID)
	}
	return out
}

func TestList_APIKeyProviderReadsTheVendorAndKeepsTheAnswer(t *testing.T) {
	v, clk := newVendor(t), &clock{t: time.Unix(1000, 0)}
	svc := llmmodels.New(llmmodels.Options{
		Keys: keys(t, "openai", secret), HTTP: v.Client(), BaseURLs: map[llm.Provider]string{llm.ProviderOpenAI: v.URL}, Now: clk.Now,
	})

	got, err := svc.List(context.Background(), "openai")
	require.NoError(t, err)
	assert.Equal(t, llmmodels.SourceAPI, got.Source)
	assert.Equal(t, []string{"gpt-live-1", "gpt-live-2"}, ids(got))
	assert.True(t, got.Has("gpt-live-2"))
	assert.False(t, got.Has("gpt-5"))

	_, err = svc.List(context.Background(), "openai")
	require.NoError(t, err)
	assert.EqualValues(t, 1, v.asks.Load(), "the second ask is answered from the kept list")

	clk.Advance(llmmodels.TTL + time.Second)
	_, err = svc.List(context.Background(), "openai")
	require.NoError(t, err)
	assert.EqualValues(t, 2, v.asks.Load(), "the list is read again after the TTL")
}

func TestList_ANewKeyIsNotAnsweredWithTheOldKeysList(t *testing.T) {
	v := newVendor(t)
	store := keys(t, "openai", secret)
	svc := llmmodels.New(llmmodels.Options{Keys: store, HTTP: v.Client(), BaseURLs: map[llm.Provider]string{llm.ProviderOpenAI: v.URL}})
	_, _ = svc.List(context.Background(), "openai")

	require.NoError(t, store.Set("openai", "sk-another-key"))
	_, _ = svc.List(context.Background(), "openai")
	assert.EqualValues(t, 2, v.asks.Load(), "the key changed: the list is another account's")

	require.NoError(t, store.Delete("openai"))
	got, err := svc.List(context.Background(), "openai")
	require.NoError(t, err)
	assert.Equal(t, llmmodels.SourceDefault, got.Source)
	assert.Equal(t, llmmodels.FallbackNoKey, got.Fallback)
	assert.EqualValues(t, 2, v.asks.Load(), "no key: nothing is asked")
}

func TestList_NoKeyShowsTheCandidatesOfMam(t *testing.T) {
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t)})
	for _, p := range []string{"anthropic", "openai", "google"} {
		got, err := svc.List(context.Background(), p)
		require.NoError(t, err)
		assert.Equal(t, llmmodels.SourceDefault, got.Source, p)
		assert.Equal(t, llmmodels.FallbackNoKey, got.Fallback, p)
		assert.Len(t, got.Models, len(llm.CandidateModels(llm.Provider(p))), p)
		assert.NotEmpty(t, got.Fallback.Message())
	}
}

func TestList_AFailedReadShowsTheCandidatesAndIsNotRetriedAtOnce(t *testing.T) {
	v, clk := newVendor(t), &clock{t: time.Unix(1000, 0)}
	v.status.Store(500)
	var logs bytes.Buffer
	svc := llmmodels.New(llmmodels.Options{
		Keys: keys(t, "openai", secret), HTTP: v.Client(), BaseURLs: map[llm.Provider]string{llm.ProviderOpenAI: v.URL}, Now: clk.Now,
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})

	got, err := svc.List(context.Background(), "openai")
	require.NoError(t, err)
	assert.Equal(t, llmmodels.SourceDefault, got.Source)
	assert.Equal(t, llmmodels.FallbackFailed, got.Fallback)
	assert.Equal(t, llm.CandidateModels(llm.ProviderOpenAI), ids(got))

	_, _ = svc.List(context.Background(), "openai")
	assert.EqualValues(t, 1, v.asks.Load(), "a provider that is down is not asked at every opening")

	v.status.Store(200)
	clk.Advance(2 * time.Minute)
	got, _ = svc.List(context.Background(), "openai")
	assert.Equal(t, llmmodels.SourceAPI, got.Source, "it recovers after a minute")

	assert.Contains(t, logs.String(), "provider=openai")
	assert.NotContains(t, logs.String(), secret)
}

func TestList_AnEmptyListIsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) }))
	t.Cleanup(srv.Close)
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t, "anthropic", secret), HTTP: srv.Client(), BaseURLs: map[llm.Provider]string{llm.ProviderAnthropic: srv.URL}})
	got, err := svc.List(context.Background(), "anthropic")
	require.NoError(t, err)
	assert.Equal(t, llmmodels.FallbackFailed, got.Fallback)
}

func TestList_UnknownProvider(t *testing.T) {
	_, err := llmmodels.New(llmmodels.Options{}).List(context.Background(), "nobody")
	assert.ErrorIs(t, err, llmmodels.ErrUnknownProvider)
}

type signin struct {
	account string
	err     error
	asks    atomic.Int32
}

func (s *signin) Account(llm.Provider) string { return s.account }
func (s *signin) Models(context.Context, llm.Provider) ([]llmmodels.Model, error) {
	s.asks.Add(1)
	return []llmmodels.Model{{ID: "gpt-plan", Name: "Plan model"}}, s.err
}

func TestList_ChatGPTProvidersFollowTheSignIn(t *testing.T) {
	for _, p := range []string{"chatgpt-subscription", "chatgpt-codex"} {
		in := &signin{account: "a@example.com"}
		svc := llmmodels.New(llmmodels.Options{ChatGPT: in})

		got, err := svc.List(context.Background(), p)
		require.NoError(t, err)
		assert.Equal(t, llmmodels.SourceAPI, got.Source, p)
		assert.Equal(t, "Plan model", got.Models[0].Name)
		_, _ = svc.List(context.Background(), p)
		assert.EqualValues(t, 1, in.asks.Load(), "%s: kept", p)

		in.account = "b@example.com"
		_, _ = svc.List(context.Background(), p)
		assert.EqualValues(t, 2, in.asks.Load(), "%s: another account is another list", p)

		in.account = ""
		got, _ = svc.List(context.Background(), p)
		assert.Equal(t, llmmodels.SourceDefault, got.Source, "%s: signed out", p)
		assert.Equal(t, llmmodels.FallbackNotSignedIn, got.Fallback)
		assert.Equal(t, llm.CandidateModels(llm.Provider(p)), ids(got))
		assert.EqualValues(t, 2, in.asks.Load(), "signed out: nothing is asked")
	}
}

func TestList_ChatGPTFailureShowsTheCandidates(t *testing.T) {
	in := &signin{account: "a@example.com", err: errors.New("boom")}
	got, err := llmmodels.New(llmmodels.Options{ChatGPT: in}).List(context.Background(), "chatgpt-subscription")
	require.NoError(t, err)
	assert.Equal(t, llmmodels.FallbackFailed, got.Fallback)
	assert.NotEmpty(t, got.Models)

	got, _ = llmmodels.New(llmmodels.Options{}).List(context.Background(), "chatgpt-codex")
	assert.Equal(t, llmmodels.FallbackNotSignedIn, got.Fallback, "no ChatGPT service at all")
}

func TestInvalidate_ReadsAgain(t *testing.T) {
	in := &signin{account: "a"}
	svc := llmmodels.New(llmmodels.Options{ChatGPT: in})
	_, _ = svc.List(context.Background(), "chatgpt-codex")
	svc.Invalidate("chatgpt-codex")
	_, _ = svc.List(context.Background(), "chatgpt-codex")
	assert.EqualValues(t, 2, in.asks.Load())
}
