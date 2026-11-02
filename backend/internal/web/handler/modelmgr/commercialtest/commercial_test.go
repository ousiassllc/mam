// Package commercialtest_test tests the commercial-use badges, filter and setting of the model manager screen (#928) over a real
// Service with the fakes of mmtest, driven as the browser's HTMX does. It has its own small harness because the directory of the
// handlers is full (linterly): the one in web/handler/modelmgr is not importable.
package commercialtest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

type screen struct {
	t      *testing.T
	rig    *mmtest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T) *screen {
	t.Helper()
	rig := mmtest.New(t)
	rig.InstallEngine()
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "m.json"), false)
	settings.ModelManager, settings.Gentest = rig.Svc, rig.Gentest
	s := &screen{t: t, rig: rig, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.ModelManager(), nil))
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

func (s *screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

// do sends an HTMX request with the form (nil: none).
func (s *screen) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *screen) get(path string) *httptest.ResponseRecorder { return s.do(http.MethodGet, path, nil) }

// page is GET /model-manager.
func (s *screen) page() string {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, paths.ModelManager(), nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// externalBox registers a ComfyUI endpoint "box" with a text-to-image model: a model the user defined, which mam did not check.
func (s *screen) externalBox() {
	s.t.Helper()
	f := comfyuitest.New(s.t)
	require.NoError(s.t, s.rig.Models.CreateEndpoint(s.t.Context(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tm, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(s.t, ok)
	require.NoError(s.t, s.rig.Models.CreateModel(s.t.Context(), localmodels.ModelInput{Endpoint: "box", Name: "t2i", Model: tm.Model}))
}

func commercialURL() string { return paths.ModelManagerPart("commercial") }

// cardOf is the markup of the card of the model id in the model list.
func cardOf(t *testing.T, body, id string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<li id="mm-model-` + regexp.QuoteMeta(id) + `" class="[^"]*" data-card[^>]*data-model="` + regexp.QuoteMeta(id) + `".*?</li>`).FindString(body)
	require.NotEmpty(t, m, "the card of "+id)
	return m
}

// #928, #954: every model card of the model manager has the commercial-use answer as an icon and data-commercial for the filter; what is not
// 商用OK says what to watch for in words, with the link to the license and the day it was read, in the modal that the card opens.
func TestCatalog_EveryModelCardHasTheCommercialMarkAndItsModalTheNoteForWhatIsNotOK(t *testing.T) {
	s := newScreen(t)
	body := s.get(paths.ModelManagerPart("models")).Body.String()
	detail := func(id string) string { return s.get(paths.ModelManagerPart("models", id, "detail")).Body.String() }

	a := cardOf(t, body, "m-a")
	assert.Contains(t, a, `data-commercial="yes"`)
	assert.Contains(t, a, `data-commercial-badge="yes"`)
	assert.Contains(t, a, "商用OK", "the accessible name of the icon")
	assert.NotContains(t, detail("m-a"), `data-testid="commercial-note"`, "商用OK has nothing to watch for")

	c := cardOf(t, body, "m-c")
	assert.Contains(t, c, `data-commercial="no"`)
	assert.Contains(t, c, `data-commercial-badge="no"`)
	assert.Contains(t, c, "商用NG")
	assert.NotContains(t, c, `data-testid="commercial-note"`, "the words are in the modal, the card keeps the icon")
	d := detail("m-c")
	assert.Contains(t, d, `data-testid="commercial-note"`)
	assert.Contains(t, d, "ライセンス原文")
	assert.Contains(t, d, "2026-11-02 に確認")
	assert.Contains(t, d, `data-commercial-badge="no"`)
}

func TestConsentDialogShowsTheBadgeNextToTheCommercialUse(t *testing.T) {
	s := newScreen(t)

	dlg := s.get(paths.ModelManagerPart("models", "m-c", "consent")).Body.String()

	assert.Regexp(t, `(?s)data-testid="mm-consent-commercial".*data-commercial-badge="no"`, dlg)
}

func TestPageHasTheSettingAndTheFilterOfTheModelList(t *testing.T) {
	s := newScreen(t)

	page := s.page()

	assert.Contains(t, page, `data-testid="mm-commercial-setting"`)
	assert.Contains(t, page, `data-commercial-filter-target="#mm-models"`)
	assert.Contains(t, page, `aria-pressed="false"`)
	assert.Regexp(t, `value="all"\s+checked`, page)
	assert.Contains(t, page, `id="mm-models"`, "the filter names the list it narrows")
}

// The setting is kept in state.json; turning it on makes the filter start at 商用OKのみ here and in the trial's model choice.
func TestSetCommercialOnlyKeepsTheSettingAndDrawsTheFilterAtItsDefault(t *testing.T) {
	s := newScreen(t)
	require.False(t, s.rig.Svc.CommercialOnly())

	on := s.do(http.MethodPut, commercialURL(), url.Values{"on": {"1"}})

	require.Equal(t, http.StatusOK, on.Code, on.Body.String())
	assert.True(t, s.rig.Svc.CommercialOnly())
	assert.Regexp(t, `value="ok"\s+checked`, on.Body.String())
	assert.Contains(t, on.Body.String(), `aria-pressed="true"`)
	assert.Contains(t, on.Body.String(), `hx-swap-oob`, "the trial's model choice is drawn again at the new default")
	assert.Regexp(t, `value="ok"\s+checked`, s.page(), "the page starts at 商用OKのみ")

	off := s.do(http.MethodPut, commercialURL(), url.Values{"on": {"0"}})

	require.Equal(t, http.StatusOK, off.Code)
	assert.False(t, s.rig.Svc.CommercialOnly())
	assert.Regexp(t, `value="all"\s+checked`, off.Body.String())
}

// A model of the trial's choice is a catalog model the user installed, and a model of an endpoint the user registered: the first says
// its license, the second is 不明 until someone checks it, and the option's text says so (only text fits in a <select>).
func TestTrialModelChoiceCarriesTheAnswerOfEachModel(t *testing.T) {
	s := newScreen(t)
	s.externalBox()

	form := s.get(paths.ModelManagerPart("trials", "form")).Body.String()

	assert.Contains(t, form, `data-commercial-filter-target="#mm-trial-model"`)
	assert.Regexp(t, `<option value="comfyui:box/t2i"[^>]*data-commercial="unknown"[^>]*>[^<]*（不明）`, form)
}
