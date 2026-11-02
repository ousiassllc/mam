package llmmodels_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/chatgpttest"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// The official sign-in's models come from GET /v1/models with the sign-in's token, which is in the request and nowhere
// else: not in the list, not in the log.
func TestFromChatGPT_TheSubscriptionListIsTheSignedInAccountsAndNeverHoldsTheToken(t *testing.T) {
	var auth atomic.Value
	var asks atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asks.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		_, _ = fmt.Fprint(w, `{"models":[{"slug":"plan-model-1","display_name":"Plan One","visibility":"list"},{"slug":"hidden","visibility":"hide"}]}`)
	}))
	t.Cleanup(api.Close)
	issuer := chatgpttest.NewServer(t)
	keys := credentialtest.New()
	keys.LimitBytes(2560)
	svc := chatgpt.New(chatgpt.Options{
		Keys: keys, DataDir: t.TempDir(), LockPath: filepath.Join(t.TempDir(), ".auth-refresh.lock"), Issuer: issuer.URL, APIBase: api.URL,
		CallbackPorts: []int{0}, Open: func(u string) error { issuer.Approve(u); return nil },
	})
	var logs bytes.Buffer
	models := llmmodels.New(llmmodels.Options{ChatGPT: llmmodels.FromChatGPT(svc), Logger: slog.New(slog.NewTextHandler(&logs, nil))})

	got, err := models.List(context.Background(), "chatgpt-subscription")
	require.NoError(t, err)
	assert.Equal(t, llmmodels.FallbackNotSignedIn, got.Fallback, "not signed in: the candidates of mam")
	assert.Zero(t, asks.Load())

	login, err := svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		l, ok := svc.Official().LoginStatus(login.ID)
		return ok && l.State == chatgpt.LoginSucceeded
	}, 10*time.Second, 10*time.Millisecond)

	got, err = models.List(context.Background(), "chatgpt-subscription")
	require.NoError(t, err)
	assert.Equal(t, llmmodels.SourceAPI, got.Source)
	assert.Equal(t, []llmmodels.Model{{ID: "plan-model-1", Name: "Plan One"}}, got.Models, "visibility list only")
	_, _ = models.List(context.Background(), "chatgpt-subscription")
	assert.EqualValues(t, 1, asks.Load(), "kept")

	token := strings.TrimPrefix(auth.Load().(string), "Bearer ")
	require.NotEmpty(t, token)
	assert.NotContains(t, fmt.Sprintf("%+v", got), token)
	assert.NotContains(t, logs.String(), token)

	_, err = svc.Official().Logout(context.Background())
	require.NoError(t, err)
	got, _ = models.List(context.Background(), "chatgpt-subscription")
	assert.Equal(t, llmmodels.FallbackNotSignedIn, got.Fallback, "signed out: the kept list is not shown")
}
