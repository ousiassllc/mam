package gentest_test

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

// screen is the generation test screen over a real Service (fake model backends, a real database and job queue), driven as the
// browser's HTMX does: the CSRF cookie and header, HX-Request.
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

// htmx adds what the browser's HTMX sends.
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

func (s *screen) get(path string) *httptest.ResponseRecorder { return s.do(http.MethodGet, path, nil) }

// page is a full-page GET (no HX-Request).
func (s *screen) page(path string) string {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// upload posts the form with an input image as the browser's multipart form does.
func (s *screen) upload(path string, form url.Values, field string, data []byte) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, vs := range form {
		for _, v := range vs {
			require.NoError(s.t, w.WriteField(k, v))
		}
	}
	if data != nil {
		part, err := w.CreateFormFile(field, "ref.png")
		require.NoError(s.t, err)
		_, _ = part.Write(data)
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return s.htmx(req)
}

// batchForm is a form of the compose screen: two prompts (the first with a negative one) on the given models.
func batchForm(models ...string) url.Values {
	return url.Values{
		"media": {"image"}, "model": models, "count": {"2"}, "aspect_ratio": {"16:9"},
		"prompt": {"a red fox", "a blue fox"}, "negative": {"blurry", ""},
	}
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

// confirmed estimates the form and returns the form the confirmation posts.
func (s *screen) confirmed(form url.Values) url.Values {
	s.t.Helper()
	rec := s.upload(paths.GenerationTestPart("estimate"), form, "", nil)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return hiddenOf(s.t, rec.Body.String())
}

var batchPath = regexp.MustCompile(`/generation-test/batches/([^/"]+)`)

// batchID reads the batch the progress section names.
func batchID(t *testing.T, body string) string {
	t.Helper()
	m := batchPath.FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return m[1]
}

// settled polls the batch as the page does until it stops asking for itself.
func (s *screen) settled(id string) string {
	s.t.Helper()
	var body string
	require.Eventually(s.t, func() bool {
		body = s.get(paths.GenerationTestPart("batches", id)).Body.String()
		return !strings.Contains(body, `hx-trigger="every 2s"`)
	}, 30*time.Second, 20*time.Millisecond, "the batch did not end")
	return body
}

// run estimates, confirms and runs the form and waits for the batch to end; it returns the batch id.
func (s *screen) run(form url.Values) string {
	s.t.Helper()
	rec := s.do(http.MethodPost, paths.GenerationTestPart("run"), s.confirmed(form))
	require.Equal(s.t, http.StatusAccepted, rec.Code, rec.Body.String())
	id := batchID(s.t, rec.Body.String())
	s.settled(id)
	return id
}

func (s *screen) results(batch string) []gentest.ResultView {
	s.t.Helper()
	b, err := s.rig.Svc.Batch(s.t.Context(), batch, true)
	require.NoError(s.t, err)
	return b.Results
}
