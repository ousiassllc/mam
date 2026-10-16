//go:build windows

package media

import (
	"context"
	"os/exec"
	"testing"
	"time"

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

// TestRunHidesConsoleWindow runs a real (trivial) child through run, the only path that starts ffmpeg
// and ffprobe, and checks the command it built: dropping hideWindow from run makes it fail.
func TestRunHidesConsoleWindow(t *testing.T) {
	var built *exec.Cmd
	orig := commandContext
	commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		built = orig(ctx, name, args...)
		return built
	}
	t.Cleanup(func() { commandContext = orig })

	_, _, err := run(context.Background(), 30*time.Second, "cmd.exe", "/c", "exit", "0")
	require.NoError(t, err)
	require.NotNil(t, built, "run must build its command with commandContext")
	require.NotNil(t, built.SysProcAttr, "run must call hideWindow")
	assert.True(t, built.SysProcAttr.HideWindow)
	assert.NotZero(t, built.SysProcAttr.CreationFlags&createNoWindow, "CREATE_NO_WINDOW must be set")
}
