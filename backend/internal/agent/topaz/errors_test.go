package topaz_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
)

// Every status of the quote becomes the failure a person can act on, in words that never hold the key (Topaz may echo it back).
func TestEstimate_TheStatusIsTheFailureTheUserReads(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		header map[string]string
		code   apperr.Code
		text   string
	}{
		{"wrong key", http.StatusUnauthorized, `{"message":"Invalid API key test-key"}`, nil, apperr.CodeAuthFailed, "認証を拒否"},
		{"no access", http.StatusForbidden, `{"errorCode":"FORBIDDEN","message":"no access"}`, nil, apperr.CodeAuthFailed, "認証を拒否"},
		{"no credits", http.StatusPaymentRequired, `{"errorCode":"INSUFFICIENT_CREDITS","message":"X-API-Key: test-key"}`, nil, apperr.CodeCreditsInsufficient, "クレジット"},
		{"rate limit", http.StatusTooManyRequests, `{"message":"slow down"}`, map[string]string{"Retry-After": "7"}, apperr.CodeRateLimited, "利用回数の上限"},
		{"maintenance", http.StatusServiceUnavailable, `{"errorCode":"SERVER_MAINTENANCE","message":"maintenance"}`, nil, apperr.CodeTopazMaintenance, "メンテナンス"},
		{"other 503", http.StatusServiceUnavailable, `{"message":"busy"}`, nil, apperr.CodeServerError, "障害"},
		{"bad request", http.StatusBadRequest, `{"errorCode":"INVALID_INPUT","errors":[{"field":"output.resolution"}]}`, nil, apperr.CodeRequestRejected, "拒否"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.onCreate = func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				writeJSON(w, tc.status, tc.body)
			}

			_, err := f.client().Estimate(background(), upscale("prob-4"))

			require.Error(t, err)
			e := errmap.Explain(err)
			assert.Equal(t, tc.code, e.Code)
			v := e.View(apperr.LangJa)
			assert.Contains(t, v.Cause, tc.text)
			assert.NotEmpty(t, v.Remedy)
			for _, text := range []string{err.Error(), v.Line(apperr.LangJa), v.Report(apperr.LangJa), e.Error()} {
				assert.NotContains(t, text, testKey, "the key is never in a message")
			}
			assert.Len(t, f.seen("POST /video/"), 1, "a quote is not asked again by itself")
		})
	}
}

func TestEstimate_ARateLimitKeepsTheRetryAfter(t *testing.T) {
	f := newFake(t)
	f.onCreate = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		writeJSON(w, http.StatusTooManyRequests, `{}`)
	}

	_, err := f.client().Estimate(background(), upscale("prob-4"))

	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 7*time.Second, pe.RetryAfter)
	assert.True(t, modeladapter.RateLimited(err))
}

func TestEstimate_ANetworkFailureHasNoURLInIt(t *testing.T) {
	f := newFake(t)
	c := f.client()
	f.api.Close()

	_, err := c.Estimate(background(), upscale("prob-4"))

	require.Error(t, err)
	assert.Equal(t, apperr.CodeConnectionFailed, errmap.Explain(err).Code)
	assert.NotContains(t, err.Error(), f.api.URL, "a transport error names the URL; the key could be in it")
}

// A failed job tells why by its errorCode: each is the cause the documentation gives, with what to do, and the job is not run again.
func TestRun_AFailedJobIsToldByItsErrorCode(t *testing.T) {
	tests := []struct {
		name   string
		status string
		code   apperr.Code
		text   string
	}{
		{"credit difference", `{"status":"Failed","errorCode":"CREDIT_DIFFERENCE","message":"differs"}`, apperr.CodeTopazCreditDiff, "見積りと大きく違って"},
		{"timeout in the queue", `{"status":"Failed","errorCode":"TIMEOUT_QUEUE"}`, apperr.CodeTopazJobTimeout, "時間切れ"},
		{"timeout of the upload", `{"status":"Failed","errorCode":"TIMEOUT_UPLOAD_SOURCE"}`, apperr.CodeTopazJobTimeout, "時間切れ"},
		{"timeout of the processing", `{"status":"Failed","errorCode":"TIMEOUT_PROCESSING"}`, apperr.CodeTopazJobTimeout, "時間切れ"},
		{"processing failure", `{"status":"Failed","errorCode":"PROCESSING_FAILURE"}`, apperr.CodeTopazJobFailed, "完了できませんでした"},
		{"hardware failure", `{"status":"Failed","errorCode":"HARDWARE_FAILURE"}`, apperr.CodeTopazJobFailed, "完了できませんでした"},
		{"unknown", `{"status":"Failed","errorCode":"UNKNOWN"}`, apperr.CodeTopazJobFailed, "完了できませんでした"},
		{"failed without a code", `{"status":"failed"}`, apperr.CodeTopazJobFailed, "完了できませんでした"},
		{"cancelled", `{"status":"Cancelled"}`, apperr.CodeTopazJobFailed, "完了できませんでした"},
		{"invalid source", `{"status":"Failed","errorCode":"INVALID_INPUT"}`, apperr.CodeTopazSourceInvalid, "元の動画を処理できませんでした"},
		{"credits ran out", `{"status":"Failed","errorCode":"INSUFFICIENT_CREDITS"}`, apperr.CodeCreditsInsufficient, "クレジット"},
		{"the key lapsed", `{"status":"Failed","errorCode":"UNAUTHORIZED","message":"key test-key"}`, apperr.CodeAuthFailed, "認証を拒否"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.onStatus = func(w http.ResponseWriter, n int) {
				if n == 1 {
					writeJSON(w, http.StatusOK, `{"status":"Processing","progress":10}`)
					return
				}
				writeJSON(w, http.StatusOK, tc.status)
			}
			in := runInput(t)
			var states []string

			n, err := f.client().Run(background(), in, func(p cloudenhance.Progress) { states = append(states, p.Status) })

			require.Error(t, err)
			assert.Zero(t, n)
			assert.ErrorIs(t, err, modeladapter.ErrAfterSubmit, "the credits are reserved: the job is not submitted again")
			e := errmap.Explain(err)
			assert.Equal(t, tc.code, e.Code)
			v := e.View(apperr.LangJa)
			assert.Contains(t, v.Cause, tc.text)
			assert.NotContains(t, v.Line(apperr.LangJa), testKey)
			assert.NotContains(t, err.Error(), testKey)
			assert.Equal(t, "processing", states[0])
			assert.Empty(t, f.seen("GET /out"), "a failed job has nothing to download")
			assert.Len(t, f.seen("PATCH /video/req-1/accept"), 1, "no second submit")
			assert.NoFileExists(t, in.DestPath)
		})
	}
}

func TestRun_TheRequestIDOfAFailedJobIsInTheDetailForTheSupport(t *testing.T) {
	f := newFake(t)
	f.onStatus = func(w http.ResponseWriter, _ int) {
		writeJSON(w, http.StatusOK, `{"status":"Failed","errorCode":"PROCESSING_FAILURE"}`)
	}

	_, err := f.client().Run(background(), runInput(t), nil)

	assert.Contains(t, errmap.Explain(err).Detail, "req-1")
}
