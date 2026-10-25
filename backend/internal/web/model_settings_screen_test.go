package web_test

import (
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
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelassignment"
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
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, file, pinned)
	settings.Models.SetHidden(hidden)
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
	req := httptest.NewRequest(method, paths.ModelSettings(), strings.NewReader(form.Encode()))
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

// modelDefaultForm is the form of the screen as it is first shown: every field holds the default.
func modelDefaultForm() url.Values {
	def := modeladapter.DefaultConfig()
	form := url.Values{}
	for p, r := range def.Images {
		for slot, t := range map[string]modeladapter.ImageTarget{modelassignment.SlotPrimary: r.Primary, modelassignment.SlotFallback: r.Fallback} {
			form.Set(modelassignment.ImageModelField(string(p), slot), string(t.Model))
			if t.Fast {
				form.Set(modelassignment.ImageFastField(string(p), slot), "1")
			}
		}
	}
	for q, r := range def.Policies {
		form.Set(modelassignment.PolicyTargetField(string(q), modelassignment.SlotPrimary), string(r.Primary.Model)+"@"+r.Primary.Resolution)
		form.Set(modelassignment.PolicyTargetField(string(q), modelassignment.SlotFallback), string(r.Fallback.Model)+"@"+r.Fallback.Resolution)
		form.Set(modelassignment.PolicyTakesField(string(q)), strconv.Itoa(r.Takes))
	}
	return form
}

// selectedOption is the value of the selected <option> of the <select> named name.
func selectedOption(t *testing.T, html, name string) string {
	t.Helper()
	sel := regexp.MustCompile(`(?s)<select[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>(.*?)</select>`).FindStringSubmatch(html)
	require.Len(t, sel, 2, name)
	opt := regexp.MustCompile(`<option value="([^"]*)"[^>]*\sselected`).FindStringSubmatch(sel[1])
	require.Len(t, opt, 2, name)
	return opt[1]
}

func optionValues(t *testing.T, html, name string) []string {
	t.Helper()
	sel := regexp.MustCompile(`(?s)<select[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>(.*?)</select>`).FindStringSubmatch(html)
	require.Len(t, sel, 2, name)
	var values []string
	for _, m := range regexp.MustCompile(`<option value="([^"]*)"`).FindAllStringSubmatch(sel[1], -1) {
		values = append(values, m[1])
	}
	return values
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
