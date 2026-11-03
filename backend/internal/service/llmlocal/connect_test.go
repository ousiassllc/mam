package llmlocal_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// server answers /v1/models like an OpenAI-compatible server.
func server(t *testing.T, status int, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var auth []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s, &auth
}

func TestTest_ListsTheModels(t *testing.T) {
	s, auth := server(t, 200, `{"data":[{"id":"gemma-4-12b"},{"id":"qwen3.8:27b"}]}`)
	svc, _ := newService(t, settingsPath(t), false)

	res, err := svc.Test(context.Background(), llmlocal.TestInput{BaseURL: s.URL + "/v1", APIKey: "draft-key"})

	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.Equal(t, []string{"gemma-4-12b", "qwen3.8:27b"}, res.Models)
	assert.Equal(t, 2, res.Count)
	assert.Contains(t, res.Message, "接続できました（モデル 2 件）")
	assert.Equal(t, []string{"Bearer draft-key"}, *auth, "the key that was typed is the one tried")
}

func TestTest_UsesTheSavedAddressAndKeyWhenGivenNone(t *testing.T) {
	s, auth := server(t, 200, `{"data":[{"id":"m"}]}`)
	svc, _ := newService(t, settingsPath(t), false)
	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: s.URL, APIKey: "saved-key"}))

	res, err := svc.Test(context.Background(), llmlocal.TestInput{})

	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.Equal(t, []string{"Bearer saved-key"}, *auth)
}

func TestTest_ANoModelServerIsConnectedWithAHint(t *testing.T) {
	s, _ := server(t, 200, `{"data":[]}`)
	svc, _ := newService(t, settingsPath(t), false)

	res, _ := svc.Test(context.Background(), llmlocal.TestInput{BaseURL: s.URL})

	assert.True(t, res.OK)
	assert.Contains(t, res.Message, "使えるモデルがありません")
}

func TestTest_ADownServerIsTheCommonErrorWithCauseAndRemedy(t *testing.T) {
	svc, _ := newService(t, settingsPath(t), false)

	res, err := svc.Test(context.Background(), llmlocal.TestInput{BaseURL: closedAddr(t)})

	require.NoError(t, err, "a failed connection is a result, not an error")
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "ローカルサーバーに接続できません")
	require.NotNil(t, res.Error)
	assert.Equal(t, apperr.CodeLocalLLMDown, res.Error.Code)
	v := res.Error.View(apperr.LangJa)
	assert.Contains(t, v.Remedy, "ベース URL")
	assert.Equal(t, "/settings/llm", v.Settings[0].Path)
}

func TestTest_AuthAndOtherAnswers(t *testing.T) {
	s401, _ := server(t, 401, `{"error":"no key"}`)
	s404, _ := server(t, 404, `not found`)
	sHTML, _ := server(t, 200, `<html>hello</html>`)
	svc, _ := newService(t, settingsPath(t), false)

	res, _ := svc.Test(context.Background(), llmlocal.TestInput{BaseURL: s401.URL})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "認証に失敗しました")
	assert.Equal(t, apperr.CodeAuthFailed, res.Error.Code)

	res, _ = svc.Test(context.Background(), llmlocal.TestInput{BaseURL: s404.URL})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "HTTP 404")

	res, _ = svc.Test(context.Background(), llmlocal.TestInput{BaseURL: sHTML.URL})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "読めませんでした")
}

func TestTest_RefusesAnAddressTheLocalRulesRefuse(t *testing.T) {
	svc, _ := newService(t, settingsPath(t), false)

	_, err := svc.Test(context.Background(), llmlocal.TestInput{BaseURL: "http://169.254.169.254/latest"})

	var invalid *llmlocal.InvalidError
	require.ErrorAs(t, err, &invalid)
}
