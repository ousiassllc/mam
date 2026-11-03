package runtimes_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
)

// A fine-tuning of the voice holds the runtime: it is not installed, updated, started or removed under it, and the screen tells why (409, not a 500).
func TestRuntimes_ARuntimeThatATrainingHoldsRefusesInstallWeightsStartAndRemoveWithTheReason(t *testing.T) {
	s := newRuntimeScreen(t)
	s.installRuntime()
	tool, err := s.Rts.Svc.BeginTool(t.Context(), lrtest.ID)
	require.NoError(t, err)
	defer tool.End()

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, runtimePart(lrtest.ID, "install")}, {http.MethodPost, runtimePart(lrtest.ID, "weights")},
		{http.MethodPost, runtimePart(lrtest.ID, "start")}, {http.MethodDelete, runtimePart(lrtest.ID)},
	} {
		rec := s.Do(tc.method, tc.path, nil)
		assert.Equal(t, http.StatusConflict, rec.Code, tc.method+" "+tc.path)
		assert.Contains(t, rec.Body.String(), "声の学習が動いています", tc.method+" "+tc.path)
	}
}
