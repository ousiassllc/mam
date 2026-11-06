package llamaserver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/llamaserver"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// ollamaRuntime is the runtime "ollama": installed, running or stopped, with no token (the Ollama of mam has no authentication).
type ollamaRuntime struct {
	view     localruntime.View
	asked    []string
	prepared int
	err      error
}

func (r *ollamaRuntime) Prepare(_ context.Context, id string) error {
	r.asked = append(r.asked, id)
	r.prepared++
	if !r.view.Installed {
		return localruntime.ErrNotInstalled
	}
	r.view.Process = proc.Status{State: proc.StateRunning, URL: "http://127.0.0.1:18081"}
	return r.err
}
func (r *ollamaRuntime) Status(_ context.Context, id string) (localruntime.View, error) {
	r.asked = append(r.asked, id)
	return r.view, r.err
}
func (r *ollamaRuntime) Token(string) (string, error) { return "", nil }
func (r *ollamaRuntime) Stop(string) error            { return nil }

func installedOllama() *ollamaRuntime {
	r := &ollamaRuntime{}
	r.view.Installed, r.view.Support.Supported, r.view.Weights.State = true, true, localruntime.WeightsReady
	return r
}

// The Ollama flavor of the server: it is the runtime "ollama", it hands out the address with no key (and is not refused for having none), it lists the models its Models function gives, and its
// words name Ollama, not llama.cpp.
func TestTheOllamaServerHasItsOwnRuntimeNoKeyAndTheModelsItHolds(t *testing.T) {
	rt := installedOllama()
	server := llamaserver.Server{Runtime: rt, ID: llamaserver.OllamaID, Label: "Ollama", NoKey: true, Models: func(context.Context) []string { return []string{"gemma4:12b", "qwen3.8:27b"} }}

	url, key, err := server.Endpoint(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:18081", url)
	assert.Empty(t, key, "no key: the server has no authentication")
	assert.Equal(t, 1, rt.prepared, "a stopped server is started by the first call")
	for _, id := range rt.asked {
		assert.Equal(t, "ollama", id)
	}
	info := server.Info(context.Background())
	assert.Equal(t, llmlocal.ManagedRunning, info.State)
	assert.Equal(t, "http://127.0.0.1:18081", info.URL)
	assert.Equal(t, []string{"gemma4:12b", "qwen3.8:27b"}, info.Models)
}

// A server that is not installed says so with the name of Ollama and does not talk about llama.cpp or a weight.
func TestTheOllamaServerNamesOllamaWhenItIsNotThere(t *testing.T) {
	rt := installedOllama()
	rt.view.Installed = false
	server := llamaserver.Server{Runtime: rt, ID: llamaserver.OllamaID, Label: "Ollama", NoKey: true}

	info := server.Info(context.Background())
	_, _, err := server.Endpoint(context.Background())

	assert.Equal(t, llmlocal.ManagedNotInstalled, info.State)
	assert.Contains(t, info.Reason, "Ollama")
	assert.NotContains(t, info.Reason, "llama.cpp")
	var unavailable *llamaserver.UnavailableError
	require.True(t, errors.As(err, &unavailable), "%v", err)
	assert.Contains(t, err.Error(), "Ollama")
	assert.NotContains(t, err.Error(), "llama.cpp")
	assert.Contains(t, llamaserver.ExplainOllama(localruntime.ErrNotInstalled), "Ollama")
}
