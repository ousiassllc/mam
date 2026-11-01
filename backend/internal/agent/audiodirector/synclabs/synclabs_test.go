package synclabs_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/synclabs"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

func syncKeys(t *testing.T, withKey bool) credential.Store {
	t.Helper()
	keyring.MockInit()
	store := credential.NewKeyring()
	if withKey {
		require.NoError(t, store.Set(audiodirector.SyncCredentialProvider, "sync-test-key"))
	}
	return store
}

func TestSyncLabs_UploadsThenPollsUntilCompletedAndDownloadsTheVideo(t *testing.T) {
	var model, key string
	var files []string
	polls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/generate":
			key = r.Header.Get("x-api-key")
			require.NoError(t, r.ParseMultipartForm(1<<20))
			model = r.FormValue("model")
			for field := range r.MultipartForm.File {
				files = append(files, field)
			}
			_, _ = w.Write([]byte(`{"id":"gen-1","status":"PENDING"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/generate/gen-1":
			polls++
			if polls < 2 {
				_, _ = w.Write([]byte(`{"id":"gen-1","status":"PROCESSING"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"gen-1","status":"COMPLETED","outputUrl":"` + srv.URL + `/out.mp4"}`))
		case r.URL.Path == "/out.mp4":
			assert.Empty(t, r.Header.Get("x-api-key"), "the key is not sent to the storage link")
			_, _ = w.Write([]byte("lip-synced-video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	sl := synclabs.New(synclabs.Config{BaseURL: srv.URL, PollInterval: time.Millisecond, LocalDownloads: true}, syncKeys(t, true), nil)

	out, err := sl.Sync(context.Background(), audiodirector.LipSyncRequest{
		Model: "sync-3", Video: audiodirector.Sample{Filename: "v.mp4", Data: []byte("v")}, Audio: audiodirector.Sample{Filename: "a.wav", Data: []byte("a")},
	})

	require.NoError(t, err)
	assert.Equal(t, []byte("lip-synced-video"), out)
	assert.Equal(t, "sync-3", model)
	assert.Equal(t, "sync-test-key", key)
	assert.ElementsMatch(t, []string{"video", "audio"}, files)
	assert.Equal(t, 2, polls)
}

func TestSyncLabs_RetriesATransientStatusOrDownloadFailureOfAnAcceptedJob(t *testing.T) {
	var polls, downloads int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"gen-1","status":"PENDING"}`))
		case r.URL.Path == "/v2/generate/gen-1":
			if polls++; polls <= 6 { // an outage longer than the old limit of three tries
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(`{"id":"gen-1","status":"COMPLETED","outputUrl":"` + srv.URL + `/out.mp4"}`))
		case r.URL.Path == "/out.mp4":
			if downloads++; downloads <= 6 {
				http.Error(w, "slow down", http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte("lip-synced-video"))
		}
	}))
	defer srv.Close()
	sl := synclabs.New(synclabs.Config{BaseURL: srv.URL, PollInterval: time.Millisecond, LocalDownloads: true}, syncKeys(t, true), nil)

	out, err := sl.Sync(context.Background(), audiodirector.LipSyncRequest{Model: "sync-3"})

	require.NoError(t, err)
	assert.Equal(t, []byte("lip-synced-video"), out)
	assert.Equal(t, 7, polls)
	assert.Equal(t, 7, downloads)
}

// A failed status request is retried until the call ends, every poll interval or, when the vendor
// asks for a long wait (Retry-After), every that long; a client error is not retried. The call ends
// after the fourth poll or a grace after the first: a Timeout from the test start can end it early.
func TestSyncLabs_RetriesTheStatusRequestUntilTheCallEndsAndHonoursRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		status     int
		retryAfter string
		manyTries  bool
	}{{http.StatusTooManyRequests, "", true}, {http.StatusTooManyRequests, "3600", false}, {http.StatusNotFound, "", false}} {
		t.Run(fmt.Sprintf("%d Retry-After %q", tc.status, tc.retryAfter), func(t *testing.T) {
			var polls atomic.Int32
			// The fourth poll ends the call; the grace only ends the Retry-After case, which polls once.
			ctx, cancel := context.WithCancel(testenv.EndsAfterFirstRequest(t, &polls, time.Second))
			defer cancel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_, _ = w.Write([]byte(`{"id":"gen-1","status":"PENDING"}`))
					return
				}
				if polls.Add(1) >= 4 && tc.manyTries {
					cancel()
				}
				w.Header().Set("Retry-After", tc.retryAfter)
				http.Error(w, "unavailable", tc.status)
			}))
			defer srv.Close()
			sl := synclabs.New(synclabs.Config{BaseURL: srv.URL, PollInterval: time.Millisecond}, syncKeys(t, true), nil)

			_, err := sl.Sync(ctx, audiodirector.LipSyncRequest{Model: "sync-3"})

			require.ErrorAs(t, err, new(*audiodirector.ChargedError))
			assert.Equal(t, tc.manyTries, polls.Load() > 3, "polls: %d", polls.Load())
		})
	}
}

func TestSyncLabs_Failures(t *testing.T) {
	t.Run("no API key", func(t *testing.T) {
		sl := synclabs.New(synclabs.Config{BaseURL: "http://127.0.0.1:1"}, syncKeys(t, false), nil)

		_, err := sl.Sync(context.Background(), audiodirector.LipSyncRequest{})

		assert.ErrorIs(t, err, audiodirector.ErrNoSyncLabsKey)
	})
	t.Run("a failed generation reports the vendor and the reason", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"id":"g","status":"FAILED","error":"no face found"}`))
		}))
		defer srv.Close()
		sl := synclabs.New(synclabs.Config{BaseURL: srv.URL}, syncKeys(t, true), nil)

		_, err := sl.Sync(context.Background(), audiodirector.LipSyncRequest{Model: "lipsync-2"})

		var perr *audiodirector.ProviderError
		require.True(t, errors.As(err, &perr))
		assert.Equal(t, audiodirector.VendorSyncLabs, perr.Vendor)
		assert.Contains(t, perr.Message, "no face found")
	})
	t.Run("HTTP error keeps the status and the key is not leaked", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"invalid key sync-test-key"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()
		sl := synclabs.New(synclabs.Config{BaseURL: srv.URL}, syncKeys(t, true), nil)

		_, err := sl.Sync(context.Background(), audiodirector.LipSyncRequest{Model: "lipsync-2"})

		var perr *audiodirector.ProviderError
		require.True(t, errors.As(err, &perr))
		assert.Equal(t, http.StatusUnauthorized, perr.Status)
	})
}

func TestSyncLabs_ADownloadFailureDoesNotLeakTheSignedOutputURL(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	storageURL := storage.URL
	storage.Close()
	var requests atomic.Int32 // the answer to the create already carries the output URL: no status request follows
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"id":"g","status":"COMPLETED","outputUrl":"` + storageURL + `/out.mp4?X-Signature=signed-secret"}`))
	}))
	defer srv.Close()
	sl := synclabs.New(synclabs.Config{BaseURL: srv.URL, PollInterval: time.Millisecond, LocalDownloads: true}, syncKeys(t, true), nil)

	_, err := sl.Sync(testenv.EndsAfterFirstRequest(t, &requests, 100*time.Millisecond), audiodirector.LipSyncRequest{Model: "sync-3"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "signed-secret")
	assert.NotContains(t, err.Error(), storageURL)
}

func TestSyncLabs_RefusesAnOutputURLThatIsNotHTTPSOrPointsToTheLocalNetwork(t *testing.T) {
	for name, output := range map[string]string{
		"plain http": "http://cdn.example.com/out.mp4",
		"loopback":   "https://127.0.0.1/out.mp4",
		"metadata":   "https://169.254.169.254/latest/meta-data",
	} {
		t.Run(name, func(t *testing.T) {
			var outputHits int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/out.mp4" {
					outputHits++
				}
				_, _ = w.Write([]byte(`{"id":"g","status":"COMPLETED","outputUrl":"` + output + `"}`))
			}))
			defer srv.Close()
			sl := synclabs.New(synclabs.Config{BaseURL: srv.URL}, syncKeys(t, true), nil)

			_, err := sl.Sync(context.Background(), audiodirector.LipSyncRequest{
				Model: "sync-3", Video: audiodirector.Sample{Filename: "v.mp4", Data: []byte("v")}, Audio: audiodirector.Sample{Filename: "a.wav", Data: []byte("a")},
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), "output URL")
			assert.Zero(t, outputHits)
		})
	}
}

// failure before it (or a FAILED / REJECTED generation) is not.
func TestSyncLabs_AFailureAfterTheJobWasAcceptedIsMarkedCharged(t *testing.T) {
	tests := []struct {
		name    string
		handler func(storageURL string) http.HandlerFunc
		charged bool
	}{
		{"output download fails", func(storage string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_, _ = w.Write([]byte(`{"id":"g","status":"PENDING"}`))
					return
				}
				_, _ = w.Write([]byte(`{"id":"g","status":"COMPLETED","outputUrl":"` + storage + `/out.mp4"}`))
			}
		}, true}, // the unreachable storage is retried until the call ends
		{"completed without an outputUrl", func(string) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":"g","status":"COMPLETED"}`))
			}
		}, true},
		{"polling fails", func(string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_, _ = w.Write([]byte(`{"id":"g","status":"PENDING"}`))
					return
				}
				http.Error(w, "boom", http.StatusInternalServerError)
			}
		}, true},
		{"timeout while polling", func(string) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":"g","status":"PROCESSING"}`))
			}
		}, true},
		{"create is refused", func(string) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusBadRequest) }
		}, false},
		{"generation FAILED", func(string) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":"g","status":"FAILED","error":"x"}`))
			}
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			storage := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			storageURL := storage.URL
			storage.Close() // the link of the output cannot be downloaded
			// The call ends a grace after the first status request, not after a Timeout from the test start.
			var polls atomic.Int32
			srv := httptest.NewServer(testenv.CountGets(&polls, tc.handler(storageURL)))
			defer srv.Close()
			sl := synclabs.New(synclabs.Config{BaseURL: srv.URL, PollInterval: 5 * time.Millisecond, LocalDownloads: true}, syncKeys(t, true), nil)

			_, err := sl.Sync(testenv.EndsAfterFirstRequest(t, &polls, 50*time.Millisecond), audiodirector.LipSyncRequest{Model: "sync-3"})

			require.Error(t, err)
			var ce *audiodirector.ChargedError
			assert.Equal(t, tc.charged, errors.As(err, &ce))
		})
	}
}
