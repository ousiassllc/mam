package providerbackends_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestAProviderWithoutItsKeyFailsBeforeAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	for _, p := range []viaprovider.Provider{viaprovider.OpenRouter, viaprovider.Fal, viaprovider.Replicate} {
		// The maker's own key is stored; the aggregator's is not: it must not be used in its place.
		_, b := backends(t, srv, p, map[string]string{"kling": "ACCESS:SECRET", "kling4": "k"})

		_, err := generate(t, b, domain.ModelKling3, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

		require.ErrorIs(t, err, modeladapter.ErrNoAPIKey, string(p))
	}
	assert.Zero(t, hits.Load())
}

func TestAModelTheProviderDoesNotServeRefusesWithoutAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	// Every key is stored, including Kling 4.0's own: the call still must not go to the maker's account.
	_, b := backends(t, srv, viaprovider.OpenRouter, map[string]string{"openrouter": "k", "kling4": "k", "kling": "A:S"})

	_, err := generate(t, b, domain.ModelKling4, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

	require.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Contains(t, err.Error(), "OpenRouter は Kling 4.0 を提供していません")
	assert.Zero(t, hits.Load())
}

func TestAResolutionTheProviderHasNoVariantOfIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.Fal, map[string]string{"fal": "k"})

	_, err := generate(t, b, domain.ModelKling4, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "480p", AspectRatio: "16:9"})

	require.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Contains(t, err.Error(), "480p")
}

func TestTheOfficialProviderKeepsTheMakersBackends(t *testing.T) {
	var kling4Hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/text-to-video/") {
			kling4Hits.Add(1)
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.Official, map[string]string{"kling4": "k", "openrouter": "k"})

	_, err := generate(t, b, domain.ModelKling4, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

	require.Error(t, err)
	assert.EqualValues(t, 1, kling4Hits.Load(), "Kling 4.0 went to its own API")
}
