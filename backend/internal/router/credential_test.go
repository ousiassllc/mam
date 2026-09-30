package router_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

func TestSettings_APIKeysAreSavedAndDeletedWithoutEverBeingShown(t *testing.T) {
	a := newApp(t)
	project := a.createProject("CM")
	store := credential.NewKeyring()
	t.Cleanup(func() { _ = store.Delete("kling") })
	const secret = "sk-very-secret-value"
	row := func(body, provider string) string {
		re := `(?s)<li[^>]*data-provider="` + provider + `".*?</li>`
		return regexpFind(t, re, body)
	}

	// The settings screen lists every provider as unset and offers a save form.
	body := a.get("/projects/"+project+"/settings", false).Body.String()
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
	assert.NotContains(t, a.get("/projects/"+project+"/settings", false).Body.String(), secret)

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
