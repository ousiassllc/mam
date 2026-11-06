package gate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/media/gate"
)

type plain struct{}

type gated struct{ on bool }

func (g gated) Available() bool { return g.on }

func TestOpen(t *testing.T) {
	assert.False(t, gate.Open(nil), "not wired")
	assert.True(t, gate.Open(plain{}), "no ffmpeg needed: always on")
	assert.False(t, gate.Open(gated{on: false}))
	assert.True(t, gate.Open(gated{on: true}))
}

// A *media.Tools is Gated: it follows the ffmpeg that is set up while mam runs, and a nil one is off.
func TestOpen_ToolsFollowReload(t *testing.T) {
	var none *media.Tools
	tools := media.NewTools()
	assert.False(t, gate.Open(none))
	assert.False(t, gate.Open(&tools), "ffmpeg is not found yet")

	_ = tools.Reload(func() (media.Tools, error) { return media.At("/bin/ffmpeg", "/bin/ffprobe"), nil })

	assert.True(t, gate.Open(&tools), "ffmpeg was set up")
}
