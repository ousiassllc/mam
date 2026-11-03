package localllm_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/localllm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// managedStub is the llama.cpp server that mam manages: whether the user chose it, and the address and key of its current start.
type managedStub struct {
	inUse atomic.Bool
	url   string
	key   string
	err   error
	asked atomic.Int32
}

func (m *managedStub) InUse() bool { return m.inUse.Load() }

func (m *managedStub) Endpoint(context.Context) (string, string, error) {
	m.asked.Add(1)
	return m.url, m.key, m.err
}

func managedBackend(t *testing.T, settings string, m *managedStub) llm.Backend {
	t.Helper()
	store := credentialtest.New()
	require.NoError(t, store.Set(string(llm.ProviderLocal), "key-of-the-settings"))
	backends := map[llm.Provider]llm.Backend{}
	localllm.Add(backends, store, nil, func() string { return settings }, localllm.WithManaged(m))
	return backends[llm.ProviderLocal]
}

// While the user has chosen the managed server, the call goes to its address with the key of its start, and the address and the key of the settings are left alone.
func TestLocalBackend_ManagedServerReplacesTheAddressAndTheKeyOfTheSettings(t *testing.T) {
	settings := newLocalServer(t, func(int, map[string]any) (int, string) { return 200, chatAnswer("from the settings") })
	managed := newLocalServer(t, func(int, map[string]any) (int, string) { return 200, chatAnswer("from llama.cpp") })
	m := &managedStub{url: managed.URL + "/v1", key: "key-of-this-start"}
	b := managedBackend(t, settings.URL, m)

	done, err := b.Complete(context.Background(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.NoError(t, err)
	assert.Equal(t, "from the settings", done.Text, "not chosen: the address of the settings, and the managed server is not even started")
	assert.Zero(t, m.asked.Load())
	assert.Equal(t, "Bearer key-of-the-settings", settings.headers[0].Get("Authorization"))

	m.inUse.Store(true)
	done, err = b.Complete(context.Background(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.NoError(t, err)
	assert.Equal(t, "from llama.cpp", done.Text)
	assert.Equal(t, "Bearer key-of-this-start", managed.headers[0].Get("Authorization"), "the key of the start, never the stored key")
	assert.Len(t, settings.bodies(), 1, "the settings server was not called again")
	assert.EqualValues(t, 1, m.asked.Load())
}

// A server that cannot be started is a server that did not answer: the common "local server down" error with the reason in it, so the retries, the
// breaker and the fallback of the Agent treat it as they treat an address that is down.
func TestLocalBackend_ManagedServerThatCannotStartIsAnUnreachableServerWithItsReason(t *testing.T) {
	m := &managedStub{err: errors.New("llama.cpp が導入されていません")}
	m.inUse.Store(true)
	b := managedBackend(t, "http://127.0.0.1:1", m)

	_, err := b.Complete(context.Background(), "m", llm.Request{User: "u"})

	require.Error(t, err)
	assert.True(t, localllm.IsUnreachable(err))
	assert.ErrorContains(t, err, "llama.cpp が導入されていません")
}

func TestLocalBackend_ManagedServerThatWasCancelledIsNotUnreachable(t *testing.T) {
	m := &managedStub{err: context.Canceled}
	m.inUse.Store(true)
	b := managedBackend(t, "http://127.0.0.1:1", m)

	_, err := b.Complete(context.Background(), "m", llm.Request{User: "u"})

	require.Error(t, err)
	assert.False(t, localllm.IsUnreachable(err), "the caller gave up: the server did not fail")
}
