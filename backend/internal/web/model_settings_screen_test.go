package web_test

import (
	"encoding/json"
	htmlpkg "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// modelScreen is the settings screen over a models.json of its own.
type modelScreen struct {
	t      *testing.T
	file   string
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newModelScreen(t *testing.T, file string, pinned bool) *modelScreen {
	t.Helper()
	return newGatedModelScreen(t, file, pinned, nil)
}

// newGatedModelScreen is newModelScreen with the age gate of the model manager: hidden says which models are adult models while the
// age confirmation is off (nil: none).
func newGatedModelScreen(t *testing.T, file string, pinned bool, hidden func(modelID string) bool) *modelScreen {
	t.Helper()
	return newConfiguredModelScreen(t, file, pinned, func(s *settingsroutes.Services) { s.Models.SetHidden(hidden) })
}

// newConfiguredModelScreen is newModelScreen with configure applied to the settings services before the screen is used.
func newConfiguredModelScreen(t *testing.T, file string, pinned bool, configure func(*settingsroutes.Services)) *modelScreen {
	t.Helper()
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, file, pinned)
	configure(settings)
	s := &modelScreen{t: t, file: file, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s
}

func (s *modelScreen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *modelScreen) request(method string, form url.Values) *httptest.ResponseRecorder {
	return s.requestTo(method, paths.ModelSettings(), form)
}

// requestTo is an HTMX request with the CSRF token, as the page's script sends one.
func (s *modelScreen) requestTo(method, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *modelScreen) page() string {
	req := httptest.NewRequest(http.MethodGet, paths.ModelSettings(), nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// imageModal is the body of the modal that edits the image purpose, as the button of its row asks for it.
func (s *modelScreen) imageModal(purpose string) string {
	rec := s.requestTo(http.MethodGet, paths.ModelImageEdit(purpose), nil)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// policyModal is the body of the modal that edits the Quality Policy.
func (s *modelScreen) policyModal(policy string) string {
	rec := s.requestTo(http.MethodGet, paths.ModelPolicyEdit(policy), nil)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// modelDefaultForm is the form of the screen as it is first shown: every field holds the default.
func modelDefaultForm() url.Values {
	def := modeladapter.DefaultConfig()
	form := url.Values{}
	for p, r := range def.Images {
		for slot, t := range map[string]modeladapter.ImageTarget{media.SlotPrimary: r.Primary, media.SlotFallback: r.Fallback} {
			form.Set(media.ImageModelField(string(p), slot), string(t.Model))
			if t.Fast {
				form.Set(media.ImageFastField(string(p), slot), "1")
			}
		}
	}
	for q, r := range def.Policies {
		form.Set(media.PolicyTargetField(string(q), media.SlotPrimary), string(r.Primary.Model)+"@"+r.Primary.Resolution)
		form.Set(media.PolicyTargetField(string(q), media.SlotFallback), string(r.Fallback.Model)+"@"+r.Fallback.Resolution)
		form.Set(media.PolicyTakesField(string(q)), strconv.Itoa(r.Takes))
	}
	return form
}

// selectedOption is the value the model field named name holds (a model picker posts its choice as a hidden input).
func selectedOption(t *testing.T, html, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<input type="hidden" name="` + regexp.QuoteMeta(name) + `" value="([^"]*)"`).FindStringSubmatch(html)
	require.Len(t, m, 2, name)
	return m[1]
}

// choices are the models the model picker lists for the model field named name ("image_<purpose>_<slot>_model", "policy_<policy>_<slot>"): what the field may be set
// to, whether or not it can be chosen now (the picker's "使えないものも表示").
func (s *modelScreen) choices(name string) []string {
	s.t.Helper()
	source := "settings-image"
	if strings.HasPrefix(name, "policy_") {
		source = "settings-video"
	}
	rec := s.requestTo(http.MethodGet, paths.ModelPickerSource(source)+"?mf=1&available_only=0", nil)
	require.Equal(s.t, http.StatusOK, rec.Code)
	var out []string
	for _, m := range regexp.MustCompile(`data-mp-values="([^"]*)"`).FindAllStringSubmatch(rec.Body.String(), -1) {
		var v map[string]string
		require.NoError(s.t, json.Unmarshal([]byte(htmlpkg.UnescapeString(m[1])), &v))
		out = append(out, v["value"])
	}
	return out
}

func numberValue(t *testing.T, html, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<input[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>`).FindString(html)
	require.NotEmpty(t, m, name)
	v := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(m)
	require.Len(t, v, 2, m)
	return v[1]
}

func isChecked(t *testing.T, html, name string) bool {
	t.Helper()
	m := regexp.MustCompile(`<input[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>`).FindString(html)
	require.NotEmpty(t, m, name)
	return regexp.MustCompile(`\schecked[\s>=]`).MatchString(m)
}
