package workflows_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// screen is the workflow screens over a real workflows.Service and a fake ComfyUI registered as the bundled
// endpoint, driven as the browser does (the CSRF cookie and header; HX-Request for the HTMX actions).
type screen struct {
	t      *testing.T
	engine http.Handler
	cookie *http.Cookie
	token  string
	comfy  *comfyuitest.Fake
	svc    *workflows.Service
	local  *localmodels.Service
}

func newScreen(t *testing.T) *screen {
	t.Helper()
	comfy := comfyuitest.New(t)
	keys := credentialtest.New()
	cfg, err := configcheck.Load("")
	require.NoError(t, err)
	opts := localbackends.Options{StagingDir: t.TempDir(), PollEvery: time.Millisecond}
	adapter := modeladapter.New(cfg, localbackends.Build(cfg, keys, opts))
	localbackends.Publish(cfg)
	t.Cleanup(func() {
		localcfg.PublishWorkflows(nil)
		localbackends.Publish(modeladapter.DefaultConfig())
	})
	local := localmodels.New(localmodels.Deps{Path: filepath.Join(t.TempDir(), "models.json"), Keys: keys, Adapter: adapter, Backends: opts})
	require.NoError(t, local.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: workflows.BundledEndpoint, Kind: localcfg.KindComfyUI, BaseURL: comfy.URL()}))

	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New()})
	wf := workflows.New(workflows.Deps{Repo: comfywf.New(db), Models: local, Adapter: adapter, Keys: keys, StagingDir: t.TempDir(), RunsDir: t.TempDir()})
	svc.Workflows = wf
	s := &screen{t: t, comfy: comfy, svc: wf, local: local, engine: router.New(svc, router.Options{Settings: &settingsroutes.Services{Workflows: wf}})}

	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Workflows(), nil))
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

func (s *screen) request(req *http.Request, csrf bool) *httptest.ResponseRecorder {
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	if csrf {
		req.Header.Set(middleware.CSRFHeaderName, s.token)
	}
	return s.send(req)
}

func (s *screen) get(path string) *httptest.ResponseRecorder {
	return s.request(httptest.NewRequest(http.MethodGet, path, nil), true)
}

func (s *screen) form(method, path string, form url.Values, csrf bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.request(req, csrf)
}

// upload is the import form: multipart, the graph pasted (api_json) or as a file (api_file).
func (s *screen) upload(path string, fields map[string]string, file string, csrf bool) *httptest.ResponseRecorder {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		require.NoError(s.t, w.WriteField(k, v))
	}
	if file != "" {
		part, err := w.CreateFormFile("api_file", "workflow_api.json")
		require.NoError(s.t, err)
		_, _ = part.Write([]byte(file))
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return s.request(req, csrf)
}

func graphOf(t *testing.T, id string) string {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok)
	return string(tpl.Model.Workflow.Graph)
}
