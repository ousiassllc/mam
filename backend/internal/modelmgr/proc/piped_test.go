package proc_test

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// fakeStdio is a child that speaks over its standard input and output: it answers every line with "echo <line>" and
// ends with "bye" when its standard input is closed. MAM_FAKE_STDIO=exit makes it exit with 7 after one line, and
// "env" makes it print the value of CODEX_HOME.
func fakeStdio() {
	switch os.Getenv("MAM_FAKE_STDIO") {
	case "env":
		fmt.Println("home=" + os.Getenv("CODEX_HOME") + " dropped=" + os.Getenv("MAM_TEST_DROP_ME"))
		return
	case "exit":
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Println("dying")
		os.Exit(7)
	}
	fmt.Fprintln(os.Stderr, "stderr is not the protocol")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		fmt.Println("echo " + sc.Text())
	}
	fmt.Println("bye")
}

func startFake(t *testing.T, mode string, env ...string) *proc.Piped {
	t.Helper()
	p, err := proc.StartPiped(proc.Spec{Command: os.Args[0], Env: append([]string{"MAM_FAKE_STDIO=" + mode}, env...)}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { p.Stop(2 * time.Second); _ = p.Stdout.Close() })
	return p
}

func TestPipedChildSpeaksOverItsStandardStreams(t *testing.T) {
	p := startFake(t, "echo")
	r := bufio.NewReader(p.Stdout)
	_, err := io.WriteString(p.Stdin, "hello\n")
	require.NoError(t, err)
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "echo hello\n", line, "stdout carries only the protocol, stderr does not")

	assert.Eventually(t, func() bool { return strings.Contains(p.LogTail(), "stderr is not the protocol") }, 5*time.Second, 10*time.Millisecond)
	p.Stop(5 * time.Second) // closing stdin is the polite end
	rest, _ := io.ReadAll(r)
	assert.Equal(t, "bye\n", string(rest), "the child saw the end of its input")
	<-p.Done()
	assert.True(t, p.Stopping())
	assert.Equal(t, 0, p.ExitCode())
}

func TestPipedChildEnvironmentAndAnUnaskedExit(t *testing.T) {
	t.Setenv("MAM_TEST_DROP_ME", "inherited")
	t.Setenv("CODEX_HOME", "/the/users/own/.codex")
	p, err := proc.StartPiped(proc.Spec{
		Command: os.Args[0], Env: []string{"MAM_FAKE_STDIO=env", "CODEX_HOME=/mam/codex-home"}, DropEnv: []string{"MAM_TEST_DROP_ME"},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { p.Stop(2 * time.Second); _ = p.Stdout.Close() })
	out, err := io.ReadAll(p.Stdout)
	require.NoError(t, err)
	assert.Equal(t, "home=/mam/codex-home dropped=", strings.TrimSpace(string(out)),
		"the child gets the CODEX_HOME mam set, not the user's, and not a dropped variable")

	q := startFake(t, "exit")
	_, err = io.WriteString(q.Stdin, "go\n")
	require.NoError(t, err)
	select {
	case <-q.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the child did not exit")
	}
	assert.False(t, q.Stopping(), "an exit nobody asked for")
	assert.Equal(t, 7, q.ExitCode())
}
