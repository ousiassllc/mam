package router_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

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
