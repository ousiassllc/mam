package local_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// screen is the settings screen over a real localmodels.Service (a models.json in a temporary directory, a
// Model Adapter that is rebuilt on a save), driven as the browser's HTMX does: the CSRF cookie and header,
// HX-Request.
type screen struct {
	t      *testing.T
	engine http.Handler
	cookie *http.Cookie
	token  string
	file   string
	keys   *credentialtest.Memory
}

// newScreen builds the screen. content is the file's content before ("" for none); pinned makes it the file
// --model-config named.
func newScreen(t *testing.T, pinned bool, content string) *screen {
	t.Helper()
	file := filepath.Join(t.TempDir(), "models.json")
	existing := ""
	if content != "" {
		require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
		existing = file
	}
	keys := credentialtest.New()
	cfg, err := configcheck.Load(existing)
	require.NoError(t, err)
	opts := localbackends.Options{StagingDir: t.TempDir()}
	adapter := modeladapter.New(cfg, localbackends.Build(cfg, keys, opts))
	localbackends.Publish(cfg)
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	local := localmodels.New(localmodels.Deps{Path: file, Pinned: pinned, Keys: keys, Adapter: adapter, Backends: opts})

	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, file, pinned)
	settings.Models.SetOnChange(func() { require.NoError(t, local.Reload()) })
	settings.LocalModels = local
	s := &screen{t: t, file: file, keys: keys, engine: router.New(svc, router.Options{Settings: settings})}

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

// page is GET /settings/local-models (the page of the category).
func (s *screen) page() string { return s.get(paths.LocalModels()) }

// imageModal is the modal that edits the models of an image purpose: what its select offers is what a local model is a choice of.
func (s *screen) imageModal(purpose string) string {
	s.t.Helper()
	return s.modal(paths.ModelImageEdit(purpose))
}

// policyModal is the modal that edits the video models of a Quality Policy.
func (s *screen) policyModal(policy string) string {
	s.t.Helper()
	return s.modal(paths.ModelPolicyEdit(policy))
}

// modal is the body of a modal, which the button of a row asks for with an HTMX request.
func (s *screen) modal(target string) string {
	s.t.Helper()
	rec := s.do(http.MethodGet, target, nil)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// assignImage chooses a model in the modal of the image purpose as the browser would: the form of the modal, one field set, saved.
func (s *screen) assignImage(purpose, field, value string) *httptest.ResponseRecorder {
	s.t.Helper()
	form := formOf(s.t, s.imageModal(purpose), "media-image-form")
	form.Set(field, value)
	return s.do(http.MethodPut, paths.ModelImage(purpose), form)
}

// assignPolicy is assignImage for a Quality Policy.
func (s *screen) assignPolicy(policy, field, value string) *httptest.ResponseRecorder {
	s.t.Helper()
	form := formOf(s.t, s.policyModal(policy), "media-policy-form")
	form.Set(field, value)
	return s.do(http.MethodPut, paths.ModelPolicy(policy), form)
}

func (s *screen) get(target string) string {
	s.t.Helper()
	rec := s.getRec(target)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// getRec is a navigation to target (no HX-Request).
func (s *screen) getRec(target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(s.cookie)
	return s.send(req)
}

// savedFile is the content of models.json ("" when there is none).
func (s *screen) savedFile() string {
	data, err := os.ReadFile(s.file)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(s.t, err)
	return string(data)
}

func (s *screen) addEndpoint(name, kind, baseURL string) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.do(http.MethodPost, paths.LocalEndpoints(), url.Values{"name": {name}, "kind": {kind}, "base_url": {baseURL}})
}

var (
	tagPattern  = regexp.MustCompile(`(?s)<(input|select|textarea|form)\b([^>]*)>`)
	attrPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9-]*)(?:="([^"]*)")?`)
)

func attrsOf(tag string) map[string]string {
	attrs := map[string]string{}
	for _, m := range attrPattern.FindAllStringSubmatch(tag, -1) {
		attrs[m[1]] = html.UnescapeString(m[2])
	}
	return attrs
}

// formOf returns what a browser would submit for the form whose data-testid is testID in body: the values of
// its text fields, hidden fields, the checked boxes and radios and the selected option of each <select> (the
// first one when none is). Disabled fields are left out.
func formOf(t *testing.T, body, testID string) url.Values {
	t.Helper()
	start := strings.Index(body, `data-testid="`+testID+`"`)
	require.GreaterOrEqual(t, start, 0, "no form %s in %s", testID, body)
	open := strings.LastIndex(body[:start], "<form")
	end := strings.Index(body[start:], "</form>")
	require.True(t, open >= 0 && end >= 0, "form %s is not closed", testID)
	form := body[open : start+end]

	values := url.Values{}
	selectRe := regexp.MustCompile(`(?s)<select\b([^>]*)>(.*?)</select>`)
	for _, m := range selectRe.FindAllStringSubmatch(form, -1) {
		name := attrsOf(m[1])["name"]
		first, picked := "", false
		for i, o := range regexp.MustCompile(`<option\b([^>]*)>`).FindAllStringSubmatch(m[2], -1) {
			a := attrsOf(o[1])
			if i == 0 {
				first = a["value"]
			}
			if _, ok := a["selected"]; ok && !picked {
				values.Set(name, a["value"])
				picked = true
			}
		}
		if !picked && name != "" {
			values.Set(name, first)
		}
	}
	for _, m := range regexp.MustCompile(`(?s)<textarea\b([^>]*)>(.*?)</textarea>`).FindAllStringSubmatch(form, -1) {
		values.Set(attrsOf(m[1])["name"], html.UnescapeString(m[2]))
	}
	for _, m := range tagPattern.FindAllStringSubmatch(form, -1) {
		if m[1] != "input" {
			continue
		}
		a := attrsOf(m[2])
		if _, off := a["disabled"]; off || a["name"] == "" {
			continue
		}
		if a["type"] == "checkbox" || a["type"] == "radio" {
			if _, on := a["checked"]; !on {
				continue
			}
		}
		values.Set(a["name"], a["value"])
	}
	return values
}

// fieldNames are the names of the fields (inputs, selects, textareas) of a form.
func fieldNames(t *testing.T, body, testID string) []string {
	t.Helper()
	v := formOf(t, body, testID)
	names := make([]string, 0, len(v))
	for k := range v {
		names = append(names, k)
	}
	return names
}

// options are the values of the options of the <select> called name in body.
func options(body, name string) []string {
	sel := regexp.MustCompile(`(?s)<select[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>(.*?)</select>`).FindStringSubmatch(body)
	if sel == nil {
		return nil
	}
	var out []string
	for _, m := range regexp.MustCompile(`<option value="([^"]*)"`).FindAllStringSubmatch(sel[1], -1) {
		out = append(out, html.UnescapeString(m[1]))
	}
	return out
}
