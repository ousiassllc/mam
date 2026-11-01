package audiodirector_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/synclabs"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func keys(t *testing.T, withKey bool) credential.Store {
	t.Helper()
	keyring.MockInit()
	store := credential.NewKeyring()
	if withKey {
		require.NoError(t, store.Set(audiodirector.CredentialProvider, "sk-test-key"))
	}
	return store
}

func syncKeys(t *testing.T, withKey bool) credential.Store {
	t.Helper()
	keyring.MockInit()
	store := credential.NewKeyring()
	if withKey {
		require.NoError(t, store.Set(audiodirector.SyncCredentialProvider, "sync-test-key"))
	}
	return store
}

func TestDefaultClientsDoNotForwardTheKeyOnARedirectToAnotherHost(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignHits.Add(1) }))
	defer foreign.Close()
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/steal", http.StatusFound)
	}))
	defer vendor.Close()

	tts := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: vendor.URL}, keys(t, true), nil)
	_, err := tts.Synthesize(context.Background(), domain.VoiceLine{Kind: domain.VoiceNarration, Text: "a"})
	require.Error(t, err)

	sl := synclabs.New(synclabs.Config{BaseURL: vendor.URL}, syncKeys(t, true), nil)
	_, err = sl.Sync(context.Background(), audiodirector.LipSyncRequest{
		Model: "sync-3", Video: audiodirector.Sample{Filename: "v.mp4", Data: []byte("v")}, Audio: audiodirector.Sample{Filename: "a.wav", Data: []byte("a")},
	})
	require.Error(t, err)

	assert.Zero(t, foreignHits.Load())
}

// Sync Labs bills a generation it accepted: a failure after the job exists is a ChargedError, a
