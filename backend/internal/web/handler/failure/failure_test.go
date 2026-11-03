package failure_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/web/handler/failure"
)

// A failure of an outside service that no handler worded reaches the toast with its cause and remedy in Japanese and the status of its class
// (#1010); a failure nobody knows stays the generic 500 (its cause is in the log, not on the screen).
func TestFallback_TellsTheCauseAndTheRemedyOfAKnownFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		want   string
	}{
		"VRAM":      {&modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Status: 422, Message: "CUDA out of memory"}, http.StatusBadGateway, "VRAM"},
		"429":       {&modeladapter.ProviderError{Vendor: "flux", Status: 429}, http.StatusTooManyRequests, "しばらく待って"},
		"own error": {fmt.Errorf("x: %w", apperr.New(apperr.CodeLicense)), http.StatusConflict, "ライセンスに、まだ同意していません"},
	} {
		t.Run(name, func(t *testing.T) {
			status, msg := failure.Fallback(tc.err)
			assert.Equal(t, tc.status, status)
			assert.Contains(t, msg, tc.want)
		})
	}
	status, msg := failure.Fallback(errors.New("sql: something odd"))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, failure.Generic, msg)
}
