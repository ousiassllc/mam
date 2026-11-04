package topaz_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/topaz"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
)

// The whole flow with the exact requests: accept, the file streamed with its length and the type, the ETag handed back, the polls, the download.
func TestRun_AcceptUploadCompletePollDownload(t *testing.T) {
	f := newFake(t)
	in := runInput(t)
	var states []cloudenhance.Progress

	n, err := f.client().Run(background(), in, func(p cloudenhance.Progress) { states = append(states, p) })

	require.NoError(t, err)
	assert.EqualValues(t, len("RESULT"), n)
	got, err := os.ReadFile(in.DestPath)
	require.NoError(t, err)
	assert.Equal(t, "RESULT", string(got))

	require.Len(t, f.seen("PATCH /video/req-1/accept"), 1)
	up := f.seen("PUT /up")
	require.Len(t, up, 1)
	assert.Equal(t, "SOURCE-BYTES", string(up[0].Body))
	assert.EqualValues(t, len("SOURCE-BYTES"), up[0].ContentLength, "streamed with its Content-Length, not chunked")
	assert.Equal(t, "video/mp4", up[0].ContentType)
	done := f.seen("PATCH /video/req-1/complete-upload/")
	require.Len(t, done, 1)
	assert.JSONEq(t, `{"uploadResults":[{"partNum":1,"eTag":"\"etag-1\""}]}`, string(done[0].Body))
	assert.Len(t, f.seen("GET /video/req-1/status"), 3)
	assert.Equal(t, []cloudenhance.Progress{{Status: "accepted", Percent: -1}, {Status: "processing", Percent: 40}, {Status: "complete", Percent: -1}}, states)
}

// The key is the credential of the API only: the presigned upload and download URLs are credentials of their own.
func TestRun_TheKeyGoesToTheAPIAndNeverToTheStorage(t *testing.T) {
	f := newFake(t)

	_, err := f.client().Run(background(), runInput(t), nil)

	require.NoError(t, err)
	api, storage := f.keyAt(false), f.keyAt(true)
	require.NotEmpty(t, api)
	for _, k := range api {
		assert.Equal(t, testKey, k)
	}
	require.Len(t, storage, 2, "the upload and the download")
	for _, k := range storage {
		assert.Empty(t, k, "no X-API-Key to a presigned URL")
	}
}

// A refusal of the accept is a refusal: nothing was reserved, so it is not marked as after the submit and the user can try again. Any other
// failure of it may have reserved the credits, so it is not repeated and is marked.
func TestRun_NoRequestThatReservesIsRepeated(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		afterSubmit bool
		code        apperr.Code
	}{
		{name: "accept refused for the credits", status: http.StatusPaymentRequired, code: apperr.CodeCreditsInsufficient},
		{name: "accept fails on the server", status: http.StatusInternalServerError, afterSubmit: true, code: apperr.CodeServerError},
		{name: "accept is limited", status: http.StatusTooManyRequests, code: apperr.CodeRateLimited},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.onAccept = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, tc.status, `{"message":"no"}`) }

			_, err := f.client().Run(background(), runInput(t), nil)

			require.Error(t, err)
			assert.Equal(t, tc.afterSubmit, errorsIsAfterSubmit(err))
			assert.Equal(t, tc.code, errmap.Explain(err).Code)
			assert.Len(t, f.seen("PATCH /video/req-1/accept"), 1, "the accept is never sent twice")
			assert.Empty(t, f.seen("PUT /up"))
		})
	}
}

func errorsIsAfterSubmit(err error) bool { return errors.Is(err, modeladapter.ErrAfterSubmit) }

func TestRun_AFailedCompleteUploadIsNotRepeatedAndIsMarked(t *testing.T) {
	f := newFake(t)
	f.onComplete = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusBadGateway, `{}`) }

	_, err := f.client().Run(background(), runInput(t), nil)

	require.Error(t, err)
	assert.True(t, errorsIsAfterSubmit(err))
	assert.Len(t, f.seen("PATCH /video/req-1/complete-upload/"), 1)
	assert.Empty(t, f.seen("GET /video/req-1/status"))
}

func TestRun_AnAcceptWithoutOneUploadURLStopsBeforeTheUpload(t *testing.T) {
	for name, body := range map[string]string{"none": `{"urls":[]}`, "no field": `{}`, "several parts": `{"urls":["https://a.example/1","https://a.example/2"]}`} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.onAccept = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, body) }

			_, err := f.client().Run(background(), runInput(t), nil)

			require.Error(t, err)
			assert.True(t, errorsIsAfterSubmit(err), "the credits are reserved already")
			assert.Empty(t, f.seen("PUT /up"))
			assert.Empty(t, f.seen("PATCH /video/req-1/complete-upload/"))
		})
	}
}

// The polls and the download are GETs of a job that is billed already: a hiccup of the server is waited out, not the end of the job.
func TestRun_AHiccupOfTheStatusIsRetriedAndTheJobFinishes(t *testing.T) {
	f := newFake(t)
	var calls atomic.Int32
	f.onStatus = func(w http.ResponseWriter, _ int) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			writeJSON(w, http.StatusServiceUnavailable, `{}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"status":"Complete","download":{"url":"`+f.storage.URL+`/out"}}`)
	}
	flaky := int32(0)
	f.onDownload = func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&flaky, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("RESULT"))
	}

	n, err := f.client().Run(background(), runInput(t), nil)

	require.NoError(t, err)
	assert.EqualValues(t, 6, n)
	assert.Len(t, f.seen("PATCH /video/req-1/accept"), 1)
	assert.Len(t, f.seen("GET /out"), 2)
}

func TestRun_TheCallerCancelsTheWaitAndNothingIsSubmittedAgain(t *testing.T) {
	f := newFake(t)
	f.onStatus = func(w http.ResponseWriter, _ int) {
		writeJSON(w, http.StatusOK, `{"status":"Processing","progress":5}`)
	}
	ctx, cancel := context.WithCancel(background())
	defer cancel()
	seen := 0

	start := time.Now()
	_, err := f.client(func(c *topaz.Config) { c.PollInterval = 20 * time.Millisecond }).Run(ctx, runInput(t), func(cloudenhance.Progress) {
		if seen++; seen == 2 {
			cancel()
		}
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, errorsIsAfterSubmit(err))
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Len(t, f.seen("PATCH /video/req-1/accept"), 1)
}

func TestRun_AJobThatDoesNotEndWithinTheTimeLimitIsATimeout(t *testing.T) {
	f := newFake(t)
	f.onStatus = func(w http.ResponseWriter, _ int) { writeJSON(w, http.StatusOK, `{"status":"Processing"}`) }

	_, err := f.client(func(c *topaz.Config) { c.Timeout = 80 * time.Millisecond }).Run(background(), runInput(t), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrTimeout)
	assert.True(t, errorsIsAfterSubmit(err))
	e := errmap.Explain(err)
	assert.Equal(t, apperr.CodeTimeout, e.Code)
	assert.Contains(t, e.View(apperr.LangJa).Remedy, "課金される場合")
}

func TestRun_WithoutAKeyNothingIsSent(t *testing.T) {
	f := newFake(t)
	c := topaz.New(topaz.Config{BaseURL: f.api.URL, LocalTransfers: true}, keyStore(t, ""), nil)

	_, err := c.Run(background(), runInput(t), nil)

	require.ErrorIs(t, err, topaz.ErrNoAPIKey)
	assert.Zero(t, f.total())
}

// The accept reserves the credits: a source file that is gone or empty is found out before it, so nothing is reserved for a job that cannot upload.
func TestRun_AMissingOrEmptySourceIsRefusedBeforeTheAccept(t *testing.T) {
	for name, prepare := range map[string]func(in *cloudenhance.RunInput){
		"missing": func(in *cloudenhance.RunInput) { require.NoError(t, os.Remove(in.SourcePath)) },
		"empty":   func(in *cloudenhance.RunInput) { require.NoError(t, os.WriteFile(in.SourcePath, nil, 0o600)) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			in := runInput(t)
			prepare(&in)

			_, err := f.client().Run(background(), in, nil)

			require.Error(t, err)
			assert.False(t, errorsIsAfterSubmit(err), "nothing was reserved, so it is not an error after the submit")
			assert.Equal(t, apperr.CodeTopazSourceInvalid, errmap.Explain(err).Code)
			assert.Zero(t, f.total(), "not even the accept was sent")
		})
	}
}
