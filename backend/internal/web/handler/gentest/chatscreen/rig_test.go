package chatscreen_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	m.Run()
}

// script answers the chat with the steps a test queues.
type script struct {
	mu    sync.Mutex
	steps []string
}

func (s *script) Complete(context.Context, llm.Agent, llm.Request) (llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		return llm.Response{}, fmt.Errorf("no more steps")
	}
	t := s.steps[0]
	s.steps = s.steps[1:]
	return llm.Response{Text: t}, nil
}

func (s *script) queue(steps ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, steps...)
}

const (
	sayStep      = `{"action":"say","text":"画像ですね。モデルを調べます"}`
	chooseStep   = `{"action":"choose","text":"どちらにしますか？","options":[{"label":"画像","detail":"1 枚 約 3 円〜"},{"label":"動画","detail":"高め"}]}`
	listStep     = `{"action":"list_models","media":"image"}`
	estimateStep = `{"action":"estimate","text":"2 モデルで見積りました","request":{"media":"image","models":["flux-2","nano-banana"],"prompts":[{"prompt":"a fox at dusk","negative_prompt":"blurry"}],"count":2,"aspect_ratio":"1:1"}}`
)

type screen struct {
	t      *testing.T
	rig    *gentesttest.Rig
	llm    *script
	engine http.Handler
	cookie *http.Cookie
	token  string
	sess   string
}

func newScreen(t *testing.T, opts ...gentesttest.Option) *screen {
	t.Helper()
	rig := gentesttest.New(t, opts...)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	s := &screen{t: t, rig: rig, llm: &script{}}
	settings.GentestChat = chat.New(chat.Deps{Gentest: rig.Svc, Agent: gentestchat.New(s.llm)})
	s.engine = router.New(svc, router.Options{Settings: settings})

	rec := s.send(httptest.NewRequest(http.MethodGet, paths.GenerationTestChat(), nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	body := rec.Body.String()
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(body)
	require.Len(t, m, 2)
	s.token = m[1]
	sm := regexp.MustCompile(`name="session" value="([0-9a-f]{32})"`).FindStringSubmatch(body)
	require.Len(t, sm, 2, "the page carries the id of a new conversation")
	s.sess = sm[1]
	return s
}

func (s *screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *screen) post(path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *screen) get(path string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	return s.send(req)
}

func (s *screen) say(text string) *httptest.ResponseRecorder {
	return s.post(paths.GenerationTestChat("messages"), url.Values{"session": {s.sess}, "content": {text}})
}

var proposalID = regexp.MustCompile(`id="gtc-p-([0-9a-f]+)"`)

func proposalOf(t *testing.T, body string) string {
	t.Helper()
	m := proposalID.FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return m[1]
}

func (s *screen) batches() int {
	s.t.Helper()
	b, err := s.rig.Svc.Batches(context.Background(), 50)
	require.NoError(s.t, err)
	return len(b)
}
