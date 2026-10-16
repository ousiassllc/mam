//go:build windows

package media

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHideWindowSetsNoConsoleFlags(t *testing.T) {
	cmd := exec.Command("ffmpeg")
	hideWindow(cmd)
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.HideWindow)
	assert.NotZero(t, cmd.SysProcAttr.CreationFlags&createNoWindow, "CREATE_NO_WINDOW must be set")
}
