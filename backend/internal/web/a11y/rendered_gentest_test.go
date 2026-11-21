package a11y_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// posePickerScreens are the dialog of the pose picker (#986) in its three states, over the pose library of posestest, whose licenses say what the shipped catalog
// says (several sentences of a license summary): the offer to install the library, the offer of the suggestive series (the main library is there, the age gate is
// open) and the gallery.
func posePickerScreens(t *testing.T, s *screentest.Screen) map[string]string {
	t.Helper()
	lib := posestest.New(t)
	s.Rig.Gentest.Poses = lib.Lib
	get := func() string {
		rec := s.Get(paths.GenerationTestPoses())
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}
	pages := map[string]string{"poses/offer": get()}
	lib.Install(t)
	pages["poses/gallery"] = get()
	lib.Open.Store(true)
	pages["poses/adult-offer"] = get()
	require.Contains(t, pages["poses/adult-offer"], "gt-pose-adult-licenses", "the offer of the suggestive series lists its licenses")
	return pages
}

// llmScript answers the chat of the generation test with the steps in order.
type llmScript struct{ steps []string }

func (s *llmScript) Complete(context.Context, llm.Agent, llm.Request) (llm.Response, error) {
	if len(s.steps) == 0 {
		return llm.Response{}, fmt.Errorf("no more steps")
	}
	text := s.steps[0]
	s.steps = s.steps[1:]
	return llm.Response{Text: text}, nil
}

// chatScreens are the chat of the generation test (#1138): its page, the thread after an estimate that the limit on the cost of one batch refuses (the app's own
// words, two sentences and more: the status line of the thread) and the thread after a question with options.
func chatScreens(t *testing.T) map[string]string {
	t.Helper()
	rig := gentesttest.New(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		cost := 0.05
		c.Gentest.MaxCostUSD = &cost
	}))
	script := &llmScript{}
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	settings.GentestChat = chat.New(chat.Deps{Gentest: rig.Svc, Agent: gentestchat.New(script)})
	engine := router.New(svc, router.Options{Settings: settings})
	send := func(req *http.Request) *httptest.ResponseRecorder {
		req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	page := send(httptest.NewRequest(http.MethodGet, paths.GenerationTestChat(), nil))
	require.Equal(t, http.StatusOK, page.Code)
	var cookie *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			cookie = c
		}
	}
	require.NotNil(t, cookie)
	body := page.Body.String()
	token := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(body)
	session := regexp.MustCompile(`name="session" value="([0-9a-f]{32})"`).FindStringSubmatch(body)
	require.Len(t, token, 2)
	require.Len(t, session, 2)
	say := func(text string) string {
		form := url.Values{"session": {session[1]}, "content": {text}}
		req := httptest.NewRequest(http.MethodPost, paths.GenerationTestChat("messages"), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.AddCookie(cookie)
		req.Header.Set(middleware.CSRFHeaderName, token[1])
		rec := send(req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return rec.Body.String()
	}

	script.steps = []string{
		`{"action":"estimate","text":"2 モデルで見積りました","request":{"media":"image","models":["flux-2","nano-banana"],"prompts":[{"prompt":"a fox at dusk"}],"count":2,"aspect_ratio":"1:1"}}`,
		`{"action":"choose","text":"どちらにしますか？","options":[{"label":"画像","detail":"1 枚 約 3 円〜"},{"label":"動画","detail":"高め"}]}`,
	}
	refused := say("見積り")
	require.Contains(t, refused, "1 回の上限", "the estimate is refused in the app's own words")
	return map[string]string{"chat/page": body, "chat/refused": refused, "chat/options": say("選ばせて")}
}
