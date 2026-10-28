package credentials_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// MiniMax has a key of its own in the same store as the other vendors (spec/architecture/model-adapters.md §9.4): the settings
// screen lists it, and it can be saved and deleted like any other.
func TestMiniMaxKeyIsListedAndCanBeSavedAndDeleted(t *testing.T) {
	r := newRig(t)

	var listed bool
	for _, p := range service.CredentialProviders {
		listed = listed || p.ID == "minimax"
	}
	require.True(t, listed, "minimax is one of the providers of the screen")

	edit := r.do(http.MethodGet, paths.CredentialEdit("minimax"), nil, true).Body.String()
	assert.Contains(t, edit, "MiniMax H3")
	assert.Contains(t, edit, `hx-put="/credentials/minimax"`)

	rec := r.do(http.MethodPut, paths.Credential("minimax"), url.Values{"api_key": {secret}}, true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `<li id="credential-row-minimax"`)
	assert.NotContains(t, rec.Body.String(), secret)
	got, err := r.keys.Get("minimax")
	require.NoError(t, err)
	assert.Equal(t, secret, got, "the key adapters read is the one saved under the vendor name")

	rec = r.do(http.MethodDelete, paths.Credential("minimax"), nil, true)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "API キーを削除しました")
	ok, err := r.keys.Has("minimax")
	require.NoError(t, err)
	assert.False(t, ok)
}

// Alibaba Cloud (Wan 3.0) has a key of its own too, in the form WORKSPACE_ID:API_KEY, and the modal says so.
func TestAlibabaKeyIsListedSaysItsFormAndCanBeSaved(t *testing.T) {
	r := newRig(t)

	var listed bool
	for _, p := range service.CredentialProviders {
		listed = listed || p.ID == "alibaba"
	}
	require.True(t, listed, "alibaba is one of the providers of the screen")

	edit := r.do(http.MethodGet, paths.CredentialEdit("alibaba"), nil, true).Body.String()
	assert.Contains(t, edit, "Wan 3.0")
	assert.Contains(t, edit, "ワークスペース ID:API キー")
	assert.Contains(t, edit, `hx-put="/credentials/alibaba"`)
	assert.NotContains(t, edit, "/test", "no free read-only call is documented for the key")

	key := "llm-ab12:" + secret
	rec := r.do(http.MethodPut, paths.Credential("alibaba"), url.Values{"api_key": {key}}, true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `<li id="credential-row-alibaba"`)
	got, err := r.keys.Get("alibaba")
	require.NoError(t, err)
	assert.Equal(t, key, got)
}
