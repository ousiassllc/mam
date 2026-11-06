// Package ffgate wraps the collaborators of the services that need ffmpeg so that they follow the ffmpeg that is set up while mam
// runs (Issue #1047; media.Tools.Reload, media/gate): each says Available() while ffmpeg is found, and the agents that use it
// ask (gate.Open) at every use instead of once at the start.
package ffgate

import (
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/media/gate"
)

// Splicer is a repair.Splicer that works while Tools hold ffmpeg: a Partial Regeneration cannot be spliced without it.
type Splicer struct {
	repair.Splicer
	Tools *media.Tools
}

// Available implements gate.Gated.
func (s Splicer) Available() bool { return s.Tools.Available() }

// Judger is a generation.Judger that works while its Frames do: the Judge samples frames with ffmpeg.
type Judger struct {
	generation.Judger
	Frames judge.FrameSource
}

// Available implements gate.Gated.
func (j Judger) Available() bool { return gate.Open(j.Frames) }

// Frames is a judge.FrameSource that works while Tools hold ffmpeg.
type Frames struct {
	judge.FrameSource
	Tools *media.Tools
}

// Available implements gate.Gated.
func (f Frames) Available() bool { return f.Tools.Available() }
