package joblist

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
)

// liveText says in words what a running ComfyUI call reports (Issue #1002): the node and its step, the time since it was queued, and
// the time the rest of the node needs. The time left is the current node's only (a workflow can have several sampling nodes), so it is
// worded as such.
func liveText(v *live.View) string {
	step := fmt.Sprintf("ステップ %d / %d", v.Step, v.MaxStep)
	if v.Node != "" {
		step = v.Node + " " + step
	}
	parts := []string{step, "経過 " + clock(v.ElapsedSeconds)}
	if v.ETASeconds != nil {
		parts = append(parts, "このノードの残り約 "+clock(*v.ETASeconds))
	}
	return strings.Join(parts, "・")
}

// clock writes seconds as "45秒", "3分20秒" or "1時間2分".
func clock(seconds int) string {
	switch {
	case seconds >= 3600:
		return fmt.Sprintf("%d時間%d分", seconds/3600, seconds%3600/60)
	case seconds >= 60:
		return fmt.Sprintf("%d分%d秒", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%d秒", seconds)
}
