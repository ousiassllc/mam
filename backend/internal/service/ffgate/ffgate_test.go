package ffgate_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/media/gate"
	"github.com/ousiassllc/mam/backend/internal/service/ffgate"
)

// Every wrapper is on exactly while the Tools hold ffmpeg, including the ffmpeg that is set up after the wrapper was made.
func TestWrappersFollowTheTools(t *testing.T) {
	tools := media.NewTools()
	wrapped := map[string]any{
		"splicer": ffgate.Splicer{Tools: &tools},
		"judger":  ffgate.Judger{Frames: ffgate.Frames{Tools: &tools}},
		"frames":  ffgate.Frames{Tools: &tools},
	}
	for name, w := range wrapped {
		assert.False(t, gate.Open(w), "%s: ffmpeg is not found yet", name)
	}

	_ = tools.Reload(func() (media.Tools, error) { return media.At("/bin/ffmpeg", "/bin/ffprobe"), nil })

	for name, w := range wrapped {
		assert.True(t, gate.Open(w), "%s: ffmpeg was set up", name)
	}

	_ = tools.Reload(func() (media.Tools, error) { return media.Tools{}, media.ErrUnavailable })

	for name, w := range wrapped {
		assert.False(t, gate.Open(w), "%s: ffmpeg was removed", name)
	}
}

// The Orchestrator asks its Judge at every use (generation.Deps.Judge): while ffmpeg is not set up the Judge is off, so Prompt Candidate Generation
// is refused and no Candidates are used; once ffmpeg is set up the same Orchestrator uses it, with no restart. Both answers come before any database access.
func TestOrchestratorFollowsTheJudgesFFmpeg(t *testing.T) {
	tools := media.NewTools()
	orch := generation.New(generation.Deps{Judge: ffgate.Judger{Frames: ffgate.Frames{Tools: &tools}}})

	_, err := orch.StartCandidates(context.Background(), "sh1", "")
	assert.ErrorIs(t, err, generation.ErrJudgeUnavailable, "ffmpeg is not found yet")
	uses, err := orch.UsesCandidates(context.Background(), domain.Shot{ID: "sh1"})
	require.NoError(t, err)
	assert.False(t, uses)

	_ = tools.Reload(func() (media.Tools, error) { return media.At("/bin/ffmpeg", "/bin/ffprobe"), nil })

	uses, err = orch.UsesCandidates(context.Background(), domain.Shot{ID: "sh1"})
	require.NoError(t, err)
	assert.True(t, uses, "ffmpeg was set up: Candidates and the Judge are on")
}
