package joblist

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
)

func TestLiveTextSaysTheNodeTheStepTheElapsedTimeAndTheTimeLeftOfTheNode(t *testing.T) {
	eta := 70
	assert.Equal(t, "KSampler ステップ 12 / 20・経過 3分20秒・このノードの残り約 1分10秒",
		liveText(&live.View{Node: "KSampler", Step: 12, MaxStep: 20, ElapsedSeconds: 200, ETASeconds: &eta}))
	assert.Equal(t, "ステップ 1 / 4・経過 45秒", liveText(&live.View{Step: 1, MaxStep: 4, ElapsedSeconds: 45}),
		"no node name, no ETA yet: nothing is made up")
}

func TestClock(t *testing.T) {
	assert.Equal(t, "0秒", clock(0))
	assert.Equal(t, "59秒", clock(59))
	assert.Equal(t, "1分0秒", clock(60))
	assert.Equal(t, "56分0秒", clock(56*60))
	assert.Equal(t, "1時間2分", clock(3600+2*60+30))
}
