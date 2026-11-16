package backendstest_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestGeminiOmniBackendSendsEveryLengthAsWholeSecondsOfTheDurationField(t *testing.T) {
	for seconds, want := range map[float64]string{3: "3s", 10: "10s", 15: "15s"} {
		var submitted map[string]any
		var downloadKey atomic.Value
		var polls atomic.Int32
		srv := omniServer(t, &submitted, &downloadKey, &polls)
		b := backendsFor(t, srv, memKeys{"google": "k"})
		req := omniRequest()
		req.DurationSeconds = seconds

		_, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, req)
		srv.Close()

		require.NoError(t, err)
		assert.Equal(t, want, submitted["response_format"].(map[string]any)["duration"])
	}
}
