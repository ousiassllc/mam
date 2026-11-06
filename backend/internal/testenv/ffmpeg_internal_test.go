package testenv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ffmpeg that polls stdin for interactive commands holds a fixture up on a busy host: every fixture
// is made with -nostdin, before the other arguments.
func TestFFmpegArgs_StartWithNostdinAndEndWithTheOutputFile(t *testing.T) {
	got := ffmpegArgs([]string{"-f", "lavfi", "-i", "sine=frequency=440:duration=1"}, "/tmp/out.wav")
	assert.Equal(t, []string{
		"-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"/tmp/out.wav",
	}, got)
}
