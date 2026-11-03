package inpaintscreen_test

import (
	"bytes"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	m.Run()
}

func TestInpaintScreen(t *testing.T) {
	s := newScreen(t)
	page := s.page(paths.GenerationTestInpaint())
	require.Contains(t, page, "mam-mask-paint")
	require.Contains(t, page, `<input type="hidden" name="model" value="`+string(gentesttest.InpaintModel)+`" data-mp-from="value">`, "the model that can redraw is in the field")
	picker := s.page(paths.ModelPickerSource("inpaint") + "?mf=1&available_only=0")
	require.Contains(t, picker, "一部だけ描き直すに対応していません", "a model that cannot is listed with why")
	require.Contains(t, picker, string(gentesttest.InpaintModel))

	model := string(gentesttest.InpaintModel)
	form := url.Values{"model": {model}, "prompt": {"a fox"}, "denoise": {"0.4"}}
	rec := s.files(paths.GenerationTestPart("inpaint", "estimate"), form, map[string][]byte{
		"source": gentesttest.PNG, "mask": gentesttest.WhitePNG,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "gt-inpaint-confirm")
	hidden := hiddenOf(t, rec.Body.String())
	rec = s.do(http.MethodPost, paths.GenerationTestPart("inpaint", "run"), hidden)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "始めました")
	first, firstBatch := s.waitNew(nil)

	detail := s.page(paths.GenerationTestPart("results", first))
	require.Contains(t, detail, `data-testid="gt-inpaint"`)
	require.Contains(t, detail, "描き直しの強さ")
	require.Contains(t, detail, "マスク")
	require.NotContains(t, detail, `data-testid="gt-inpaint-source"`)

	again := url.Values{"model": {model}, "prompt": {"a fox"}, "denoise": {"0.4"}, "ref": {"history::" + first}}
	rec = s.files(paths.GenerationTestPart("inpaint", "estimate"), again, map[string][]byte{"mask": gentesttest.WhitePNG})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = s.do(http.MethodPost, paths.GenerationTestPart("inpaint", "run"), hiddenOf(t, rec.Body.String()))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	second, _ := s.waitNew([]string{firstBatch})
	detail = s.page(paths.GenerationTestPart("results", second))
	require.Contains(t, detail, `data-testid="gt-inpaint-source"`)
	require.Contains(t, detail, first)

	bad := url.Values{"model": {string(gentesttest.PlainModel)}, "prompt": {"a fox"}, "denoise": {"0.4"}}
	rec = s.files(paths.GenerationTestPart("inpaint", "estimate"), bad, map[string][]byte{
		"source": gentesttest.PNG, "mask": gentesttest.WhitePNG,
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "対応していない")
}

type screen struct {
	t      *testing.T
	rig    *gentesttest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T, opts ...gentesttest.Option) *screen {
	t.Helper()
	rig := gentesttest.New(t, append([]gentesttest.Option{gentesttest.WithInpaintModel()}, opts...)...)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	s := &screen{t: t, rig: rig, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.GenerationTestInpaint(), nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = html.UnescapeString(m[1])
	return s
}

func (s *screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *screen) htmx(req *http.Request) *httptest.ResponseRecorder {
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *screen) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.htmx(req)
}

func (s *screen) page(path string) string {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

func (s *screen) files(path string, form url.Values, files map[string][]byte) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, vs := range form {
		for _, v := range vs {
			require.NoError(s.t, w.WriteField(k, v))
		}
	}
	for name, data := range files {
		part, err := w.CreateFormFile(name, name+".png")
		require.NoError(s.t, err)
		_, _ = part.Write(data)
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return s.htmx(req)
}

func hiddenOf(t *testing.T, body string) url.Values {
	t.Helper()
	form := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		form.Add(html.UnescapeString(m[1]), html.UnescapeString(m[2]))
	}
	require.NotEmpty(t, form.Get("estimate_id"), body)
	return form
}

func (s *screen) waitNew(known []string) (resultID, batchID string) {
	s.t.Helper()
	list, err := s.rig.Svc.Batches(s.t.Context(), 10)
	require.NoError(s.t, err)
	for _, b := range list {
		if contains(known, b.BatchID) {
			continue
		}
		st := s.rig.Wait(b.BatchID)
		require.Equal(s.t, gentest.StateSucceeded, st.State)
		require.NotEmpty(s.t, st.Results)
		return st.Results[0].ID, b.BatchID
	}
	s.t.Fatal("no new batch")
	return "", ""
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
