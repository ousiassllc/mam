package stack_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func send(t *testing.T, method, path string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return do(newEngine(t), req)
}

// A URL without a route answers like an unknown target of a page route (spec/api/endpoints.md §0):
// the full page with the way back, or a toast for an HTMX request, never gin's plain text.
func TestNoRoute_PageRequestGetsTheErrorPageAndHTMXAToast(t *testing.T) {
	rec := send(t, http.MethodGet, "/no-such-page", false)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "<main")
	assert.Contains(t, rec.Body.String(), "プロジェクト一覧へ")
	assert.Contains(t, rec.Body.String(), "ページが見つかりません: /no-such-page")

	rec = send(t, http.MethodGet, "/no-such-page", true)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Equal(t, "false", rec.Header().Get("HX-Push-Url"))
	assert.Contains(t, rec.Body.String(), `hx-swap-oob="beforeend"`)
	assert.Contains(t, rec.Body.String(), "ページが見つかりません: /no-such-page")
	assert.NotContains(t, rec.Body.String(), "<main")
}

func TestNoMethod_PageRequestGetsTheErrorPageAndHTMXAToast(t *testing.T) {
	rec := send(t, http.MethodOptions, "/projects", false)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Contains(t, rec.Body.String(), "<main")
	assert.Contains(t, rec.Body.String(), "プロジェクト一覧へ")

	rec = send(t, http.MethodOptions, "/projects", true)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, rec.Body.String(), `hx-swap-oob="beforeend"`)
	assert.Contains(t, rec.Body.String(), "この URL ではこの操作はできません")
}

func TestNoRoute_APIStaysAProblemJSON(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		rec := send(t, http.MethodGet, "/api/v1/no-such", htmx)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
		assert.Contains(t, rec.Body.String(), `"status":404`)
		assert.Empty(t, rec.Header().Get("HX-Reswap"))
		assert.NotContains(t, rec.Body.String(), "<main")
	}

	rec := send(t, http.MethodOptions, "/api/v1/health", false)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"status":405`)
}
