package comfyui_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
)

// A job that mam queued is in live.Default with the step the server reported while it runs (the tools that show the state of
// the GPU read it there), and is gone when the call ends.
func TestARunningJobIsRecordedForTheGPUStatusAndRemovedAtTheEnd(t *testing.T) {
	f := comfyuitest.New(t)
	f.Hold = make(chan struct{})
	b := backend(t, f, "txt2img-sdxl", nil)
	done := make(chan error, 1)
	go func() {
		_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x"})
		done <- err
	}()

	var promptID string
	require.Eventually(t, func() bool {
		if s := f.Submitted(); len(s) > 0 {
			promptID = str(s[0]["prompt_id"])
		}
		if promptID == "" {
			return false
		}
		p, ok := live.Default.Lookup("box", promptID)
		return ok && p.MaxStep == 10
	}, 5*time.Second, 5*time.Millisecond, "the step the server reports reaches the registry")
	p, _ := live.Default.Lookup("box", promptID)
	assert.Equal(t, "KSampler", p.Node)
	assert.Equal(t, 5, p.Step, "another client's progress is not recorded")
	assert.Positive(t, p.Elapsed, "the time since the call was queued runs while the job runs")
	require.NotNil(t, p.View())
	assert.GreaterOrEqual(t, p.View().ElapsedSeconds, 0)

	close(f.Hold)
	require.NoError(t, <-done)
	_, ok := live.Default.Lookup("box", promptID)
	assert.False(t, ok)
}
