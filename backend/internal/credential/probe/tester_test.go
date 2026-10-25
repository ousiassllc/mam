package probe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/probe"
)

type memKeys map[string]string

func (m memKeys) Get(p string) (string, error) {
	if k, ok := m[p]; ok {
		return k, nil
	}
	return "", credential.ErrNotFound
}
func (m memKeys) Set(p, k string) error      { m[p] = k; return nil }
func (m memKeys) Has(p string) (bool, error) { _, ok := m[p]; return ok, nil }
func (m memKeys) Delete(p string) error      { delete(m, p); return nil }

// fakeProvider answers every call with status and remembers the Authorization header it was called with.
func fakeProvider(t *testing.T, status int) (url string, lastKey *atomic.Value, calls *atomic.Int32) {
	t.Helper()
	lastKey, calls = &atomic.Value{}, &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		lastKey.Store(r.Header.Get("Authorization"))
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, lastKey, calls
}

// A typed key is tried and not saved; a blank one tries the saved key; what comes back carries no key.
func TestTester_TriesTheTypedKeyOrTheSavedOneAndSavesNothing(t *testing.T) {
	url, lastKey, calls := fakeProvider(t, http.StatusOK)
	keys := memKeys{}
	tester := probe.NewTester(keys)
	tester.Prober = probe.Prober{BaseURLs: map[string]string{"openai": url}}

	res, err := tester.TestConnection(context.Background(), "openai", "  sk-typed  ")
	require.NoError(t, err)
	assert.Equal(t, probe.Result{Outcome: probe.Accepted, Status: 200}, res)
	assert.Equal(t, "Bearer sk-typed", lastKey.Load())
	assert.Empty(t, keys, "a test never saves")

	_, err = tester.TestConnection(context.Background(), "openai", "")
	var input *probe.InputError
	require.ErrorAs(t, err, &input, "no typed key and no saved key")
	assert.EqualValues(t, 1, calls.Load(), "nothing is sent without a key")

	keys["openai"] = "sk-saved"
	_, err = tester.TestConnection(context.Background(), "openai", "   ")
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk-saved", lastKey.Load())
}

func TestTester_RefusesWhatCannotBeTriedBeforeAnythingIsSent(t *testing.T) {
	url, _, calls := fakeProvider(t, http.StatusOK)
	tester := probe.NewTester(memKeys{})
	tester.Prober = probe.Prober{BaseURLs: map[string]string{"openai": url}}

	for _, provider := range []string{"kling", "seedance", "synclabs", "typesafe", "nobody"} {
		_, err := tester.TestConnection(context.Background(), provider, "k")
		assert.ErrorIs(t, err, probe.ErrUnsupported, provider)
		assert.False(t, tester.CanTestConnection(provider), provider)
	}
	var input *probe.InputError
	_, err := tester.TestConnection(context.Background(), "openai", "two words")
	assert.ErrorAs(t, err, &input)
	assert.Zero(t, calls.Load())
}
