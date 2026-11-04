package topaz_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/topaz"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
)

// The URLs of the answers are remote input: without the test switch, only https URLs of public hosts are used, and nothing is sent to the
// others (the key would not go there either, but the file would).
func TestRun_AnUploadURLThatIsNotHTTPSOfAPublicHostIsRefused(t *testing.T) {
	for _, target := range []string{"http://example.com/up", "https://127.0.0.1/up", "https://localhost/up", "https://[::1]/up", "ftp://example.com/up", "https://10.0.0.5/up"} {
		t.Run(target, func(t *testing.T) {
			f := newFake(t)
			f.uploadURL = target
			c := topaz.New(topaz.Config{BaseURL: f.api.URL, PollInterval: 1}, keyStore(t, testKey), nil)

			_, err := c.Run(background(), runInput(t), nil)

			require.Error(t, err)
			assert.True(t, errorsIsAfterSubmit(err))
			assert.Equal(t, apperr.CodeTopazTransferFailed, errmap.Explain(err).Code)
			assert.Empty(t, f.seen("PUT /up"))
			assert.Empty(t, f.seen("PATCH /video/req-1/complete-upload/"))
		})
	}
}

// The test client (httpclient.New: the API client, which the fake uses for the files too) refuses a redirect to another host. The production
// client of the files (safefetch.NewClient) instead follows up to 5 redirects that each pass safefetch.CheckURL; it cannot connect to a local
// httptest server, so that behaviour is pinned in safefetch (TestNewClientFollowsRedirectsOnlyToHTTPSURLsOfPublicHosts) and not here. No key goes to a file host either way.
func TestRun_ADownloadThatRedirectsToAnotherHostIsRefused(t *testing.T) {
	var otherHit atomic.Bool
	hit := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { otherHit.Store(true) }))
	t.Cleanup(hit.Close)
	f := newFake(t)
	f.onDownload = func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, hit.URL+"/x", http.StatusFound) }
	in := runInput(t)

	_, err := f.client().Run(background(), in, nil)

	require.Error(t, err)
	assert.False(t, otherHit.Load(), "the redirect was not followed")
	assert.Equal(t, apperr.CodeTopazTransferFailed, errmap.Explain(err).Code)
	assert.NoFileExists(t, in.DestPath)
}

// A job that completed with an empty file is a failed transfer (not a success with a 0-byte video), and no file stays.
func TestRun_AnEmptyDownloadIsAFailedTransfer(t *testing.T) {
	f := newFake(t)
	f.onDownload = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	in := runInput(t)

	n, err := f.client().Run(background(), in, nil)

	require.Error(t, err)
	assert.Zero(t, n)
	assert.Equal(t, apperr.CodeTopazTransferFailed, errmap.Explain(err).Code)
	assert.True(t, errorsIsAfterSubmit(err))
	assert.NoFileExists(t, in.DestPath)
}

// A result larger than the cap is not kept: by its length when it is announced, and by what arrives when it is not.
func TestRun_ADownloadOverTheCapIsRefusedAndRemoved(t *testing.T) {
	for name, announce := range map[string]bool{"announced length": true, "unannounced": false} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.onDownload = func(w http.ResponseWriter, _ *http.Request) {
				body := []byte("0123456789")
				if announce {
					w.Header().Set("Content-Length", "10")
					_, _ = w.Write(body)
					return
				}
				w.(http.Flusher).Flush() // chunked: no length
				_, _ = w.Write(body)
			}
			in := runInput(t)

			n, err := f.client(func(c *topaz.Config) { c.MaxDownloadBytes = 5 }).Run(background(), in, nil)

			require.Error(t, err)
			assert.Zero(t, n)
			assert.Equal(t, apperr.CodeTopazTransferFailed, errmap.Explain(err).Code)
			assert.True(t, errorsIsAfterSubmit(err), "the job ran: it is not submitted again")
			_, statErr := os.Stat(in.DestPath)
			assert.True(t, os.IsNotExist(statErr), "no partial file stays")
		})
	}
}

func TestRun_AResultExactlyAtTheCapIsKept(t *testing.T) {
	f := newFake(t)
	in := runInput(t)

	n, err := f.client(func(c *topaz.Config) { c.MaxDownloadBytes = 6 }).Run(background(), in, nil)

	require.NoError(t, err)
	assert.EqualValues(t, 6, n)
}

func TestRun_AnUploadThatIsRefusedOrHasNoETagIsAFailedTransferAndTheJobIsNotSubmittedAgain(t *testing.T) {
	tests := map[string]func(w http.ResponseWriter, r *http.Request){
		"forbidden": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"no etag":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.onUpload = handler

			_, err := f.client().Run(background(), runInput(t), nil)

			require.Error(t, err)
			e := errmap.Explain(err)
			assert.Equal(t, apperr.CodeTopazTransferFailed, e.Code, "a 403 of the storage is not the API key's")
			assert.True(t, errorsIsAfterSubmit(err))
			assert.Empty(t, f.seen("PATCH /video/req-1/complete-upload/"))
			assert.Len(t, f.seen("PATCH /video/req-1/accept"), 1)
		})
	}
}
