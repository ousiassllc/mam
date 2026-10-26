//go:build !windows

package proc_test

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nilSignal() os.Signal { return syscall.Signal(0) }

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(nilSignal()) == nil && !zombie(pid)
}

// zombie: the child ended but has not been reaped yet (it still answers signal 0).
func zombie(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	_, after, ok := strings.Cut(string(data), ") ")
	return ok && strings.HasPrefix(after, "Z")
}

// The child is killed when mam is killed (SIGKILL: no chance to stop it): the engine must not outlive the
// application, or the uninstaller could not delete its files. A supervisor process plays mam.
func TestTheChildEndsWhenMamIsKilled(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("needs /proc (Linux)")
	}
	sup := exec.Command(os.Args[0])
	sup.Env = append(os.Environ(), "MAM_FAKE_SUPERVISOR=1")
	out, err := sup.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, sup.Start())
	t.Cleanup(func() { _ = sup.Process.Kill(); _ = sup.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err, "the supervisor started the child")
	fields := strings.Fields(line)
	require.Len(t, fields, 2, line)
	pid, err := strconv.Atoi(fields[1])
	require.NoError(t, err)
	require.True(t, alive(pid))

	require.NoError(t, sup.Process.Kill())
	_ = sup.Wait()
	assert.Eventually(t, func() bool { return !alive(pid) }, 10*time.Second, 50*time.Millisecond, "the child was killed with its parent")
}
