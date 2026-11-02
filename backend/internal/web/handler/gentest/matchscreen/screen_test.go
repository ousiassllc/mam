package matchscreen_test

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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	m.Run()
}

// screen is the prompt match screen over a real Service (fake model backends and LLM, a real database and job queue), driven as the
// browser's HTMX does: the CSRF cookie and header, HX-Request.
type screen struct {
	t      *testing.T
	rig    *gentesttest.Rig
	llm    *gentesttest.FakeMatchLLM
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T, scores []int, withMatch bool, opts ...gentesttest.Option) *screen {
	t.Helper()
	rig := gentesttest.New(t, opts...)
	f := &gentesttest.FakeMatchLLM{Scores: scores, Err: map[string]error{}}
	if withMatch {
		rig.UseMatch(f)
	}
	return screenOver(t, rig, f)
}

// newFaceScreen is a screen whose match mixes the face into the similarity (faces and mix as the Judge setting says).
func newFaceScreen(t *testing.T, scores []int, faces facesim.Engine, mix imagematch.FaceMix, opts ...gentesttest.Option) *screen {
	t.Helper()
	rig := gentesttest.New(t, opts...)
	f := &gentesttest.FakeMatchLLM{Scores: scores, Err: map[string]error{}}
	rig.UseMatcher(imagematch.New(f, gentesttest.Target, gentesttest.Target).UseFaces(faces, mix))
	return screenOver(t, rig, f)
}

func screenOver(t *testing.T, rig *gentesttest.Rig, f *gentesttest.FakeMatchLLM) *screen {
	t.Helper()
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	s := &screen{t: t, rig: rig, llm: f, engine: router.New(svc, router.Options{Settings: settings})}
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

func (s *screen) htmx(req *http.Request) *httptest.ResponseRecorder {
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *screen) get(path string) *httptest.ResponseRecorder {
	return s.htmx(httptest.NewRequest(http.MethodGet, path, nil))
}

func (s *screen) post(path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.htmx(req)
}

// upload posts the form with the reference image as the browser's multipart form does.
func (s *screen) upload(path string, form url.Values, data []byte) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, vs := range form {
		for _, v := range vs {
			require.NoError(s.t, w.WriteField(k, v))
		}
	}
	if data != nil {
		part, err := w.CreateFormFile("upload", "ref.png")
		require.NoError(s.t, err)
		_, _ = part.Write(data)
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return s.htmx(req)
}

// hiddenOf reads the hidden fields of the confirmation back as the form the browser would post.
func hiddenOf(t *testing.T, body string) url.Values {
	t.Helper()
	form := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		form.Add(html.UnescapeString(m[1]), html.UnescapeString(m[2]))
	}
	require.NotEmpty(t, form.Get("estimate_id"), body)
	return form
}

func matchForm(mode string) url.Values {
	return url.Values{"model": {"flux-2"}, "mode": {mode}, "target_score": {"85"}, "max_rounds": {"4"}}
}

// batchID is the match the service has (the one the test started).
func (s *screen) batchID() string {
	s.t.Helper()
	list, err := s.rig.Svc.Batches(s.t.Context(), 10)
	require.NoError(s.t, err)
	require.Len(s.t, list, 1)
	return list[0].BatchID
}

// settled polls the match as the page does until it stops asking for itself.
func (s *screen) settled(id string) string {
	s.t.Helper()
	var body string
	require.Eventually(s.t, func() bool {
		body = s.get(paths.GenerationTestPart("match", "batches", id)).Body.String()
		return !strings.Contains(body, `hx-trigger="every 2s"`)
	}, 30*time.Second, 20*time.Millisecond, "the match did not end")
	return body
}

func (s *screen) estimate(form url.Values) (*httptest.ResponseRecorder, string) {
	s.t.Helper()
	rec := s.upload(paths.GenerationTestPart("match", "estimate"), form, gentesttest.PNG)
	return rec, rec.Body.String()
}
