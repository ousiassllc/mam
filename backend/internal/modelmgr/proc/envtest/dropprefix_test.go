// Package envtest checks the environment a child of proc gets (Spec.DropEnvPrefixes). It is a package of its own because the directory of proc is at its line budget.
package envtest_test

import (
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

// TestMain turns the test binary into the child that prints the variables the tests look at, when MAM_FAKE_ENVPRINT is set.
func TestMain(m *testing.M) {
	if os.Getenv("MAM_FAKE_ENVPRINT") != "" {
		fmt.Println("origins=" + os.Getenv("OLLAMA_ORIGINS") + " noprune=" + os.Getenv("OLLAMA_NOPRUNE") + " host=" + os.Getenv("OLLAMA_HOST") + " other=" + os.Getenv("OLLAMAX_KEEP"))
		return
	}
	os.Exit(m.Run())
}

// The OLLAMA_ variables of the user (OLLAMA_ORIGINS=*, OLLAMA_NOPRUNE, ...) are those of another Ollama: the child of a spec that drops the prefix does not inherit them (#1110), and
// the variables mam names itself (OLLAMA_HOST) arrive with mam's value, not the user's. A variable that only looks like the prefix (OLLAMAX_KEEP) is not dropped.
func TestChildDoesNotInheritTheVariablesOfADroppedPrefix(t *testing.T) {
	t.Setenv("OLLAMA_ORIGINS", "*")
	t.Setenv("OLLAMA_NOPRUNE", "1")
	t.Setenv("OLLAMA_HOST", "0.0.0.0:11434")
	t.Setenv("OLLAMAX_KEEP", "kept")
	p, err := proc.StartPiped(proc.Spec{
		Command: os.Args[0], Env: []string{"MAM_FAKE_ENVPRINT=1", "OLLAMA_HOST=127.0.0.1:41234"}, DropEnvPrefixes: []string{"OLLAMA_"},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { p.Stop(2 * time.Second); _ = p.Stdout.Close() })
	out, err := io.ReadAll(p.Stdout)
	require.NoError(t, err)
	assert.Equal(t, "origins= noprune= host=127.0.0.1:41234 other=kept", strings.TrimSpace(string(out)))
}

// A spec without the prefix keeps the whole environment of mam (the other runtimes are not changed).
func TestChildInheritsTheEnvironmentWhenNoPrefixIsDropped(t *testing.T) {
	t.Setenv("OLLAMA_ORIGINS", "*")
	p, err := proc.StartPiped(proc.Spec{Command: os.Args[0], Env: []string{"MAM_FAKE_ENVPRINT=1"}}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { p.Stop(2 * time.Second); _ = p.Stdout.Close() })
	out, err := io.ReadAll(p.Stdout)
	require.NoError(t, err)
	assert.Contains(t, string(out), "origins=* ")
}
