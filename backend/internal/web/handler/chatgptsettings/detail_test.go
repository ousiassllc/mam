package chatgptsettings_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// detail is the body of the detail modal of a card (the explanations, the risks and the terms that are not on the card).
func (s *screen) detail(kind string) string {
	return s.modal(paths.ChatGPTDetail(kind))
}

// actFromModal is act as the form of a detail modal sends it: its target is the body of the modal.
func (s *screen) actFromModal(method, target string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "mam-modal-body")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func TestChatGPTSettings_TheDetailOfAnUnknownCardIsA404Modal(t *testing.T) {
	s := newScreen(t, true)
	req := httptest.NewRequest(http.MethodGet, paths.ChatGPTDetail("nobody"), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.send(req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "対象が見つかりません")
	// A request that is not HTMX (a typed address, a reload) gets the page the card is on.
	rec = s.send(httptest.NewRequest(http.MethodGet, paths.ChatGPTDetail("official"), nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="chatgpt-card-official"`)
}
