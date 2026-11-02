package localcfg_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The timeout of a ComfyUI endpoint is how long a job may show no progress, and a call of it is bounded only by the
// largest timeout; any other kind has no stall limit and its timeout bounds the call.
func TestStallLimitAndCallLimitOfAnEndpoint(t *testing.T) {
	comfy := localcfg.Endpoint{Kind: localcfg.KindComfyUI, TimeoutSeconds: 600}
	assert.Equal(t, 10*time.Minute, comfy.StallLimit())
	assert.Equal(t, localcfg.MaxTimeoutSeconds, comfy.CallLimit())
	assert.Equal(t, 3*time.Hour, localcfg.Endpoint{Kind: localcfg.KindComfyUI}.StallLimit(), "the default timeout")

	compat := localcfg.Endpoint{Kind: localcfg.KindOpenAICompat, TimeoutSeconds: 600}
	assert.Zero(t, compat.StallLimit())
	assert.Equal(t, 600, compat.CallLimit())
	assert.Equal(t, localcfg.DefaultTimeoutCompat, localcfg.Endpoint{Kind: localcfg.KindOpenAICompat}.CallLimit())
}
