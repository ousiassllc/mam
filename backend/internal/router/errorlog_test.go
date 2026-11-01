package router_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// A failing service must leave its cause in the log and show the error page, not a blank 500.
func TestProjects_ServiceFailureIsLoggedAndRendersErrorPage(t *testing.T) {
	testenv.Parallel(t)
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New()})
	var logs bytes.Buffer
	engine := router.New(svc, router.Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	require.NoError(t, db.Close()) // every query now fails with "database is closed"

	rec := do(engine, httptest.NewRequest(http.MethodGet, "/projects", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), `name="csrf-token"`)
	assert.Contains(t, rec.Body.String(), "内部エラーが発生しました")
	assert.Contains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "database is closed")

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.Header.Set("HX-Request", "true")
	rec = do(engine, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, rec.Body.String(), "内部エラーが発生しました")
}

// An unexpected error of a JSON API handler is a generic 500: the cause is logged, not sent to the client.
func TestAPI_ServiceFailureIsAGeneric500WhoseCauseIsLogged(t *testing.T) {
	testenv.Parallel(t)
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New()})
	var logs bytes.Buffer
	engine := router.New(svc, router.Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	require.NoError(t, db.Close()) // every query now fails with "database is closed"

	rec := do(engine, httptest.NewRequest(http.MethodGet, "/api/v1/shots/x/takes", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.NotContains(t, rec.Body.String(), "database is closed")
	assert.Contains(t, rec.Body.String(), "Internal Server Error")
	assert.Contains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "database is closed")
}

// An external API answering 429 reaches the browser as 429 (spec/api/errors.md), not as a 502.
func TestAudio_RateLimitOfTheReasoningLLMIs429AndOtherProviderFailuresStay502(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/edit-timelines/"+e.timeline().ID+"/judge", nil).Code)
	require.Equal(t, "AUDIO", e.state())

	e.llm.audioErr = &llm.APIError{Provider: llm.ProviderAnthropic, Status: http.StatusTooManyRequests, Message: "slow down"}
	rec := e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, rec.Body.String(), "レート制限")
	assert.Equal(t, "AUDIO", e.state(), "nothing changed")

	e.llm.audioErr = &llm.APIError{Provider: llm.ProviderAnthropic, Status: http.StatusInternalServerError, Message: "boom"}
	assert.Equal(t, http.StatusBadGateway, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
}
