package credential_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

const testTimeout = 50 * time.Millisecond

// A call the OS store never answers (a Secret Service waiting for an unlock prompt) comes back with
// ErrUnavailable instead of hanging (#734).
func TestWithTimeout_AHungCallFailsWithErrUnavailable(t *testing.T) {
	t.Parallel()
	store := credential.WithTimeout(credentialtest.NewBlocking(t), testTimeout)

	assert.ErrorIs(t, store.Set("kling", "sk-one"), credential.ErrUnavailable)
	_, err := store.Get("kling")
	assert.ErrorIs(t, err, credential.ErrUnavailable)
	assert.ErrorIs(t, store.Delete("kling"), credential.ErrUnavailable)
	_, err = store.Has("kling")
	assert.ErrorIs(t, err, credential.ErrUnavailable)
}

func TestWithTimeout_AnAnsweringStoreIsPassedThrough(t *testing.T) {
	t.Parallel()
	store := credential.WithTimeout(credentialtest.New(), time.Minute)

	require.NoError(t, store.Set("kling", "sk-one"))
	got, err := store.Get("kling")
	require.NoError(t, err)
	assert.Equal(t, "sk-one", got)
	has, err := store.Has("kling")
	require.NoError(t, err)
	assert.True(t, has)
	require.NoError(t, store.Delete("kling"))
	_, err = store.Get("kling")
	assert.ErrorIs(t, err, credential.ErrNotFound, "the store's own errors are not turned into ErrUnavailable")
}

// The settings panel status gives up on the first hung provider instead of asking each of them for the
// full timeout.
func TestCredentialService_StatusFailsOnceWhenTheStoreHangs(t *testing.T) {
	t.Parallel()
	svc := service.NewCredentialService(credential.WithTimeout(credentialtest.NewBlocking(t), testTimeout))

	start := time.Now()
	_, err := svc.Status()
	assert.ErrorIs(t, err, credential.ErrUnavailable)
	assert.Less(t, time.Since(start), 10*testTimeout, "Status stops at the first provider that times out")
}

// A key store that never answers costs the settings screen only its API key panel: the screen is
// still served, with the panel saying the key store is unavailable (#734).
func TestSettings_RendersWhenTheKeyStoreNeverAnswers(t *testing.T) {
	t.Parallel()
	keys := credential.WithTimeout(credentialtest.NewBlocking(t), testTimeout)
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: keys})
	project, err := svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "動画を作りたい"})
	require.NoError(t, err)
	engine := router.New(svc, router.Options{})

	req := httptest.NewRequest(http.MethodGet, "/projects/"+project.ID+"/settings", nil)
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.ServeHTTP(rec, req)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the settings screen hangs on a key store that never answers")
	}

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="credential-unavailable"`)
	assert.Contains(t, rec.Body.String(), `id="policy-`, "the rest of the screen is still there")
}
