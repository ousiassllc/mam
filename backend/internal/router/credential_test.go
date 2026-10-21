package router_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettings_APIKeysAreSavedAndDeletedWithoutEverBeingShown(t *testing.T) {
	a := newApp(t)
	store := a.keys
	const secret = "sk-very-secret-value"
	row := func(body, provider string) string {
		re := `(?s)<li[^>]*data-provider="` + provider + `".*?</li>`
		return regexpFind(t, re, body)
	}

	// The global settings screen lists every provider as unset and offers a save form.
	body := a.get("/settings", false).Body.String()
	for _, p := range []string{"anthropic", "openai", "google", "kling", "seedance", "runway", "elevenlabs", "synclabs", "typesafe"} {
		assert.Contains(t, body, `hx-put="/credentials/`+p+`"`, p)
	}
	assert.Contains(t, row(body, "kling"), "未設定")

	// Saving takes effect in the store at once and the panel only says "設定済み".
	rec := a.form(http.MethodPut, "/credentials/kling", url.Values{"api_key": {"  " + secret + "  "}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got, err := store.Get("kling")
	require.NoError(t, err)
	assert.Equal(t, secret, got, "the key is trimmed and stored")
	assert.Contains(t, row(rec.Body.String(), "kling"), "設定済み")
	assert.NotContains(t, rec.Body.String(), secret)
	assert.NotContains(t, a.get("/settings", false).Body.String(), secret)

	// A refused key keeps the stored one and says why.
	for _, bad := range []string{"", "   ", "two words"} {
		rec = a.form(http.MethodPut, "/credentials/kling", url.Values{"api_key": {bad}})
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%q", bad)
		assert.Contains(t, rec.Body.String(), `role="alert"`)
	}
	got, err = store.Get("kling")
	require.NoError(t, err)
	assert.Equal(t, secret, got)

	// Only the managed providers can be written.
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/credentials/not-a-provider", url.Values{"api_key": {"x"}}).Code)
	ok, err := store.Has("not-a-provider")
	require.NoError(t, err)
	assert.False(t, ok)

	// Deleting removes it; deleting again is fine.
	rec = a.form(http.MethodDelete, "/credentials/kling", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, row(rec.Body.String(), "kling"), "未設定")
	ok, err = store.Has("kling")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, http.StatusOK, a.form(http.MethodDelete, "/credentials/kling", nil).Code)
}

// regexpFind returns the first match of pattern in body.
func regexpFind(t *testing.T, pattern, body string) string {
	t.Helper()
	m := regexp.MustCompile(pattern).FindString(body)
	require.NotEmpty(t, m, "no match for %s", pattern)
	return m
}

// Without a usable OS key store (no Secret Service, locked) only the API key panel is lost: the rest
// of the settings screen, which needs no key, still renders.
func TestSettings_RendersWithoutAKeyStore(t *testing.T) {
	a := newApp(t)
	a.keys.FailWith(errors.New("secret service is not available"))

	rec := a.get("/settings", false)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="credential-unavailable"`)
	assert.NotContains(t, body, `data-testid="credential-row"`)
	assert.Contains(t, body, "<h1", "the rest of the screen is still there")

	// The HTMX fragment degrades the same way.
	rec = a.get("/settings", true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="credential-unavailable"`)
}

// Saving or deleting a key with an unusable OS key store answers the panel that says so (503), not
// the generic internal error (#324).
func TestSettings_SavingAndDeletingKeysWithoutAKeyStoreAnswersThePanelWith503(t *testing.T) {
	a := newApp(t)
	a.keys.FailWith(errors.New("secret service is not available"))

	for _, rec := range []*httptest.ResponseRecorder{
		a.form(http.MethodPut, "/credentials/kling", url.Values{"api_key": {"sk-abc"}}),
		a.form(http.MethodDelete, "/credentials/kling", nil),
	} {
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `data-testid="credential-unavailable"`)
		assert.NotContains(t, rec.Body.String(), "内部エラー")
	}
}
