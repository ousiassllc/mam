package service_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestErrRateLimited_MatchesA429FromEveryExternalModelAPIAndNothingElse(t *testing.T) {
	tooMany := http.StatusTooManyRequests
	for name, cause := range map[string]error{
		"reasoning LLM": &llm.APIError{Status: tooMany},
		"model adapter": &modeladapter.ProviderError{Status: tooMany},
		"audio vendor":  &audiodirector.ProviderError{Status: tooMany},
		"wrapped":       fmt.Errorf("planner: %w", &llm.APIError{Status: tooMany}),
	} {
		assert.ErrorIs(t, &service.AIError{Reason: "x", Err: cause}, service.ErrRateLimited, name)
	}
	for name, cause := range map[string]error{
		"server error": &llm.APIError{Status: http.StatusInternalServerError},
		"bad request":  &modeladapter.ProviderError{Status: http.StatusBadRequest},
		"no cause":     nil,
		"other":        errors.New("boom"),
	} {
		assert.NotErrorIs(t, &service.AIError{Reason: "x", Err: cause}, service.ErrRateLimited, name)
	}
}
