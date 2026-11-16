package workflows

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"

// takesAudio reports whether the node is the one whose "audio" input is the sound file of a speech-to-video workflow (Issue #1153): the first LoadAudio node of a
// video graph that has a literal one.
func takesAudio(n localcfg.Node, kind localcfg.Media, spec localcfg.WorkflowSpec) bool {
	return kind == localcfg.MediaVideo && n.ClassType == "LoadAudio" && literal(n, "audio") && !has(spec, localcfg.ParamAudio)
}
