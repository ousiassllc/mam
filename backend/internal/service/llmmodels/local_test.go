package llmmodels_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// closedURL is the address of a local server that is not running.
func closedURL(t *testing.T) func() string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	url := "http://" + l.Addr().String()
	require.NoError(t, l.Close())
	return func() string { return url }
}

// localServer answers GET /v1/models like Ollama and counts the asks.
type localServer struct {
	*httptest.Server
	asks atomic.Int32
	mu   sync.Mutex
	auth []string
}

func newLocalServer(t *testing.T, body string) *localServer {
	t.Helper()
	s := &localServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.asks.Add(1)
		s.mu.Lock()
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.mu.Unlock()
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

const ollamaModels = `{"object":"list","data":[{"id":"gemma-4-12b","object":"model"},{"id":"nomic-embed-text:latest"},{"id":"qwen3.8:27b"}]}`

func TestLocal_ListsTheModelsTheServerHasWithoutAKey(t *testing.T) {
	s := newLocalServer(t, ollamaModels)
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: func() string { return s.URL }})

	got, err := svc.List(context.Background(), "local")

	require.NoError(t, err)
	assert.Equal(t, llmmodels.SourceAPI, got.Source)
	assert.Equal(t, []string{"gemma-4-12b", "qwen3.8:27b"}, ids(got))
	assert.Equal(t, modelfilter.Usable(), got.Availability, "connected and listed: usable, with no API key")
	assert.Equal(t, []string{""}, s.auth, "no key saved: no Authorization header")

	_, _ = svc.List(context.Background(), "local")
	assert.EqualValues(t, 1, s.asks.Load(), "the answer is kept")
}

func TestLocal_TheSavedKeyIsSentAndAChangeOfKeyOrAddressReadsAgain(t *testing.T) {
	a, b := newLocalServer(t, ollamaModels), newLocalServer(t, `{"data":[{"id":"other"}]}`)
	store := keys(t, "local", "local-key")
	url := a.URL
	svc := llmmodels.New(llmmodels.Options{Keys: store, LocalBaseURL: func() string { return url }})

	got, _ := svc.List(context.Background(), "local")
	assert.Equal(t, []string{"gemma-4-12b", "qwen3.8:27b"}, ids(got))
	assert.Equal(t, []string{"Bearer local-key"}, a.auth)

	url = b.URL
	got, _ = svc.List(context.Background(), "local")
	assert.Equal(t, []string{"other"}, ids(got), "another address is another server: the list of the one before is not answered")

	require.NoError(t, store.Set("local", "another-key"))
	_, _ = svc.List(context.Background(), "local")
	assert.EqualValues(t, 2, b.asks.Load(), "another key reads the list again")
}

func TestLocal_ADownServerIsUnusableWithTheReasonAndTheScreenThatFixesIt(t *testing.T) {
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: closedURL(t)})

	got, err := svc.List(context.Background(), "local")

	require.NoError(t, err)
	assert.Equal(t, llmmodels.SourceDefault, got.Source)
	assert.Equal(t, llmmodels.FallbackLocalDown, got.Fallback)
	assert.Equal(t, "ローカルサーバーに接続できません", got.Fallback.Message())
	assert.Empty(t, got.Models)
	assert.False(t, got.Availability.Available)
	assert.Equal(t, modelfilter.ReasonEndpoint, got.Availability.Reason)
	assert.Contains(t, got.Availability.Message, "ローカルサーバーに接続できません")
	assert.Equal(t, "/settings/llm", got.Availability.Route)

	a, err := svc.Availability("local")
	require.NoError(t, err)
	assert.Equal(t, got.Availability, a, "the availability follows the read that failed (kept for a minute)")
}

func TestLocal_AvailabilityBeforeAnyReadIsUsable(t *testing.T) {
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: closedURL(t)})

	a, err := svc.Availability("local")

	require.NoError(t, err)
	assert.True(t, a.Available, "no network is used: the models are asked for when a picker opens")
}

func TestLocal_AServerWithNoModelIsConnectedNotDown(t *testing.T) {
	s := newLocalServer(t, `{"object":"list","data":[]}`)
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: func() string { return s.URL }})

	got, err := svc.List(context.Background(), "local")

	require.NoError(t, err)
	assert.Equal(t, llmmodels.SourceAPI, got.Source)
	assert.Empty(t, got.Models)
	assert.True(t, got.Availability.Available)
}

func TestLocal_CandidatesAreLocalOriginAndSelectableByFilter(t *testing.T) {
	s := newLocalServer(t, ollamaModels)
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: func() string { return s.URL }})

	shown, err := svc.Candidates(context.Background(), modelfilter.Default())
	require.NoError(t, err)
	require.Len(t, shown, 2, "no key anywhere else: only the local server's models are offered")
	for _, c := range shown {
		assert.Equal(t, "local", c.Provider)
		it := c.Item()
		assert.Equal(t, modelfilter.OriginLocal, it.Origin)
		assert.True(t, it.Installed)
		assert.Contains(t, it.Kinds, modelfilter.KindLLM)
	}

	local, err := svc.Candidates(context.Background(), modelfilter.Condition{Origins: []modelfilter.Origin{modelfilter.OriginLocal}, Text: "qwen"})
	require.NoError(t, err)
	require.Len(t, local, 1)
	assert.Equal(t, "qwen3.8:27b", local[0].Model.ID)
}

func TestLocal_AnAssignmentAtADownServerIsAWarning(t *testing.T) {
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: closedURL(t)})
	_, _ = svc.List(context.Background(), "local")

	got := svc.Warnings([]llmmodels.Target{{Agent: "writer", Role: "primary", Provider: "local", Model: "gemma-4-12b"}})

	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "ローカルサーバーに接続できません")
	assert.Equal(t, "/settings/llm", got[0].Availability.Route)
}
