package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
)

// flakyStore is a key store that works until it is told to fail the reads after a write (a locked
// keychain, a Secret Service that went away).
type flakyStore struct {
	keys     map[string]string
	setErr   error
	deleteEr error
	hasErr   error
}

func (s *flakyStore) Set(provider, key string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.keys[provider] = key
	s.hasErr = errors.New("secret service went away")
	return nil
}

func (s *flakyStore) Get(provider string) (string, error) {
	if k, ok := s.keys[provider]; ok {
		return k, nil
	}
	return "", credential.ErrNotFound
}

func (s *flakyStore) Delete(provider string) error {
	if s.deleteEr != nil {
		return s.deleteEr
	}
	delete(s.keys, provider)
	s.hasErr = errors.New("secret service went away")
	return nil
}

func (s *flakyStore) Has(provider string) (bool, error) {
	if s.hasErr != nil {
		return false, s.hasErr
	}
	_, ok := s.keys[provider]
	return ok, nil
}

func do(t *testing.T, store *flakyStore, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := service.NewCredentialService(store)
	r := gin.New()
	r.PUT("/credentials/:provider", handler.SaveCredential(svc))
	r.DELETE("/credentials/:provider", handler.DeleteCredential(svc))
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// A key that was saved is not reported as failed when the key store cannot be read back (#324).
func TestSaveCredential_SavedKeyIsNotReportedAsFailedWhenTheStoreCannotBeReadAfterwards(t *testing.T) {
	store := &flakyStore{keys: map[string]string{}}
	rec := do(t, store, http.MethodPut, "/credentials/kling", url.Values{"api_key": {"sk-abc"}})
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="credential-unavailable"`)
	assert.Contains(t, rec.Body.String(), `data-testid="credential-notice"`)
	assert.Equal(t, "sk-abc", store.keys["kling"])
}

func TestDeleteCredential_DeletedKeyIsNotReportedAsFailedWhenTheStoreCannotBeReadAfterwards(t *testing.T) {
	store := &flakyStore{keys: map[string]string{"kling": "sk-abc"}}
	rec := do(t, store, http.MethodDelete, "/credentials/kling", nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="credential-notice"`)
	assert.NotContains(t, store.keys, "kling")
}

// A key store that refuses the write or the delete answers 503 with the panel and a clear message,
// not a generic internal error.
func TestCredential_RefusedByTheStoreAnswers503WithAClearMessage(t *testing.T) {
	store := &flakyStore{keys: map[string]string{"kling": "sk-abc"}, setErr: errors.New("locked"), deleteEr: errors.New("locked"), hasErr: errors.New("locked")}
	for _, c := range []struct{ method, form string }{{http.MethodPut, "sk-new"}, {http.MethodDelete, ""}} {
		rec := do(t, store, c.method, "/credentials/kling", url.Values{"api_key": {c.form}})
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, c.method)
		assert.Contains(t, rec.Body.String(), `data-testid="credential-unavailable"`, c.method)
		assert.NotContains(t, rec.Body.String(), "内部エラー", c.method)
	}
	store.hasErr = nil
	rec := do(t, store, http.MethodPut, "/credentials/kling", url.Values{"api_key": {"sk-new"}})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "保存できませんでした", "the readable store still says which key failed")
	require.Equal(t, "sk-abc", store.keys["kling"])
}
