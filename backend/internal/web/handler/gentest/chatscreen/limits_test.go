package chatscreen_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestAnEstimateOverTheLimitsHasNoRunButton(t *testing.T) {
	s := newScreen(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		cost := 0.05
		c.Gentest.MaxCostUSD = &cost
	}))
	s.llm.queue(estimateStep, `{"action":"say","text":"高すぎるので 1 モデルにしましょう"}`)
	rec := s.say("見積り")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, `data-testid="gtc-proposal"`, "no card to press for a batch over max_cost_usd")
	assert.NotContains(t, body, `data-testid="gtc-run"`)
	assert.Contains(t, body, "1 回の上限 0.0500 USD")
	assert.Contains(t, body, "高すぎるので")
	assert.Equal(t, 0, s.batches())
}

func TestANewEstimateReplacesTheOldCardWhereItIs(t *testing.T) {
	s := newScreen(t)
	s.llm.queue(estimateStep)
	first := proposalOf(t, s.say("見積り").Body.String())
	s.llm.queue(`{"action":"estimate","request":{"media":"image","models":["flux-2"],"prompts":[{"prompt":"a fox"}]}}`)
	body := s.say("flux-2 だけで").Body.String()
	second := regexp.MustCompile(`id="gtc-p-([0-9a-f]+)"`).FindAllStringSubmatch(body, -1)
	require.Len(t, second, 2, "the new card, and the old one again out of band")
	assert.Contains(t, body, fmt.Sprintf(`id="gtc-p-%s"`, first))
	assert.Contains(t, body, "新しい見積りに置き換わりました")
	assert.Equal(t, 1, strings.Count(body, `data-testid="gtc-run"`), "one live card")
}

func TestTheMessageIsCheckedAndTheLLMFailureIsALine(t *testing.T) {
	s := newScreen(t)
	rec := s.say("   ")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "1〜2000 文字")

	rec = s.say("狐") // the script has no step: the LLM failed
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-failed="true"`)
	assert.Contains(t, rec.Body.String(), "LLM の割り当て")
}

func TestWithoutAnLLMRouteThePageSaysSoAndNothingCanBePosted(t *testing.T) {
	rig := gentesttest.New(t)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	engine := router.New(svc, router.Options{Settings: settings})
	do := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	page := do(http.MethodGet, paths.GenerationTestChat())
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), `data-testid="gtc-unavailable"`)
	assert.NotContains(t, page.Body.String(), `data-testid="gtc-composer"`)
	assert.GreaterOrEqual(t, do(http.MethodPost, paths.GenerationTestChat("messages")).Code, http.StatusBadRequest, "no route answers a message (the guard of a post without a token may answer first)")
}

func TestTheBoxAndTheServiceAgreeOnTheLongestMessage(t *testing.T) {
	assert.Equal(t, chat.MaxMessageRunes, view.MaxMessageRunes)
	s := newScreen(t)
	body := s.send(httptest.NewRequest(http.MethodGet, paths.GenerationTestChat(), nil)).Body.String()
	assert.Contains(t, body, fmt.Sprintf(`maxlength="%d"`, chat.MaxMessageRunes))
}
