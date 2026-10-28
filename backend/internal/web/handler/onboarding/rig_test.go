package onboarding_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/chatgpttest"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	guide "github.com/ousiassllc/mam/backend/internal/service/onboarding"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// rig is the router over the settings of a test with the first-run guide, driven as the browser's HTMX does (the CSRF
// cookie and header). Everything lives in temporary directories and fake services: no real key store, no network.
type rig struct {
	t        *testing.T
	engine   http.Handler
	dir      string
	llmFile  string
	keys     credential.Store
	cookie   *http.Cookie
	token    string
	settings *settingsroutes.Services
}

type config struct {
	goos      string
	look      func(string) (string, error)
	noGuide   bool
	chatGPT   bool
	keys      credential.Store
	flagBytes string // the content of onboarding.json before the first request ("" for no file)
}

func foundBoth(file string) (string, error) { return "/usr/bin/" + file, nil }

func newRig(t *testing.T, mutate ...func(*config)) *rig {
	t.Helper()
	cfg := config{goos: "linux", look: foundBoth, keys: credentialtest.New()}
	for _, m := range mutate {
		m(&cfg)
	}
	r := &rig{t: t, dir: t.TempDir(), keys: cfg.keys}
	r.llmFile = filepath.Join(r.dir, "llm.json")
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: cfg.keys})
	r.settings = settingsroutes.New(r.llmFile, false, filepath.Join(r.dir, "models.json"), false)
	flag := filepath.Join(r.dir, guide.FileName)
	if cfg.flagBytes != "" {
		require.NoError(t, os.WriteFile(flag, []byte(cfg.flagBytes), 0o600))
	}
	if !cfg.noGuide {
		r.settings.Onboarding = guide.New(flag, guide.WithGOOS(cfg.goos), guide.WithLookPath(cfg.look))
	}
	if cfg.chatGPT {
		home := t.TempDir()
		t.Setenv("HOME", home) // nothing may be read or written below the user's own home
		t.Setenv("USERPROFILE", home)
		auth := chatgpttest.NewServer(t)
		r.settings.ChatGPT = chatgpt.New(chatgpt.Options{
			Keys: credentialtest.New(), DataDir: t.TempDir(), LockPath: filepath.Join(t.TempDir(), ".auth-refresh.lock"), Issuer: auth.URL,
			Now:   time.Now,
			Sleep: func(context.Context, time.Duration) error { return nil },
			Open:  func(u string) error { auth.Approve(u); return nil },
		})
	}
	r.engine = router.New(svc, router.Options{Settings: r.settings})
	if !cfg.noGuide {
		rec := r.get(paths.OnboardingStep(paths.OnboardingFlow))
		require.Equal(t, http.StatusOK, rec.Code)
		for _, c := range rec.Result().Cookies() {
			if c.Name == middleware.CSRFCookieName {
				r.cookie = c
			}
		}
		require.NotNil(t, r.cookie)
		m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
		require.Len(t, m, 2)
		r.token = m[1]
	}
	return r
}

func (r *rig) flagFile() string { return filepath.Join(r.dir, guide.FileName) }

func (r *rig) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	r.engine.ServeHTTP(rec, req)
	return rec
}

func (r *rig) get(target string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return r.send(req)
}

// act sends a write as HTMX does, with the CSRF cookie and header.
func (r *rig) act(method, target string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(r.cookie)
	req.Header.Set(middleware.CSRFHeaderName, r.token)
	return r.send(req)
}

func (r *rig) page(slug string) string {
	r.t.Helper()
	rec := r.get(paths.OnboardingStep(slug))
	require.Equal(r.t, http.StatusOK, rec.Code, slug)
	return rec.Body.String()
}

var (
	titleRe = regexp.MustCompile(`<title>([^<]*)</title>`)
	h2Re    = regexp.MustCompile(`<h2 id="onboarding-step-title"[^>]*>([^<]*)</h2>`)
)
