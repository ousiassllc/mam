package gatedtest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// The words that blame the token (A, B) or the access (C): none of them may answer a failure that is none of those.
var blamesTheToken = []string{hfguide.NoToken, hfguide.InvalidToken, "ライセンスに同意", "アクセスを申請", "無効"}

// A probe that fails for a reason that is not the token or the access (the file is gone, the rate limit, a failing server, no
// connection) is told as that, never as an invalid token or a missing consent, and makes no job.
func TestAFailedProbeDoesNotBlameTheTokenAndMakesNoJob(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int // 0: the site is down (the connection fails)
		header map[string]string
		want   string
	}{
		{"not found", http.StatusNotFound, nil, "見つかりません（HTTP 404）"},
		{"rate limited", http.StatusTooManyRequests, map[string]string{"Retry-After": "3600"}, "レート制限"},
		{"server error", http.StatusInternalServerError, nil, "応答できません（HTTP 500）"},
		{"unavailable", http.StatusServiceUnavailable, nil, "応答できません（HTTP 503）"},
		{"no connection", 0, nil, "ネットワークを確かめて"},
	} {
		t.Run(c.name, func(t *testing.T) {
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range c.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(c.status)
			}))
			if c.status == 0 {
				site.Close()
			}
			t.Cleanup(site.Close)
			r := mmtest.New(t, mmtest.WithGatedModel(), mmtest.HFAuth(), mmtest.HFBase(site.URL))
			r.InstallEngine()
			require.NoError(t, r.Svc.SetHuggingFaceToken(r.HF.Token))

			_, err := r.Svc.InstallModel(context.Background(), mmtest.GatedModelID, &mmtest.GatedLicense)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
			for _, w := range blamesTheToken {
				assert.NotContains(t, err.Error(), w)
			}
			assert.NotContains(t, err.Error(), r.HF.Token)
			noJob(t, r)
		})
	}
}
