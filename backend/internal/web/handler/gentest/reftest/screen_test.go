// Package reftest_test tests the input images of the generation test screen (#933) over a real Service with the local reference
// models of gentesttest, driven as the browser's HTMX does. It has its own small harness because the directory of the handlers
// is full (linterly): the one in web/handler/gentest is not importable.
package reftest_test

import (
	"bytes"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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

type screen struct {
	t      *testing.T
	rig    *gentesttest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T, opts ...gentesttest.Option) *screen {
	t.Helper()
	rig := gentesttest.New(t, opts...)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	s := &screen{t: t, rig: rig, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.GenerationTest(), nil))
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

// page is a full-page GET.
func (s *screen) page(path string) string {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// estimate posts the form with the files of the field upload, as the browser's multipart form does.
func (s *screen) estimate(form url.Values, files ...[]byte) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, vs := range form {
		for _, v := range vs {
			require.NoError(s.t, w.WriteField(k, v))
		}
	}
	for _, data := range files {
		part, err := w.CreateFormFile("upload", "ref.png")
		require.NoError(s.t, err)
		_, _ = part.Write(data)
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, paths.GenerationTestPart("estimate"), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// confirmed estimates the form and returns the form the confirmation posts.
func (s *screen) confirmed(form url.Values) url.Values {
	s.t.Helper()
	rec := s.estimate(form)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return hiddenOf(s.t, rec.Body.String())
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

// historyRef is an image of the history: a result of a finished batch.
func (s *screen) historyRef() string {
	s.t.Helper()
	ref, err := s.rig.Confirmed(gentest.Request{
		Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}},
	})
	require.NoError(s.t, err)
	b := s.rig.Wait(ref.BatchID)
	require.NotEmpty(s.t, b.Results)
	return "history::" + b.Results[0].ID
}

func batchForm(models ...string) url.Values {
	return url.Values{"media": {"image"}, "model": models, "count": {"1"}, "aspect_ratio": {"1:1"}, "prompt": {"a red fox"}}
}
