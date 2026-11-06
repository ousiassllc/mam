package llmlocal_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// managedOllama is the Ollama of mam as the service sees it: the address it answers at, the models it holds, whether it runs.
type managedOllama struct {
	url     string
	state   llmlocal.ManagedState
	models  []string
	stopped int
}

func (m *managedOllama) Endpoint(context.Context) (string, string, error) {
	return m.url + "/v1", "", nil
}
func (m *managedOllama) Stop(context.Context) error { m.stopped++; return nil }
func (m *managedOllama) Info(context.Context) llmlocal.ManagedInfo {
	return llmlocal.ManagedInfo{State: m.state, URL: m.url, Models: m.models}
}

func useOllama(t *testing.T, m *managedOllama) *llmlocal.Service {
	t.Helper()
	svc, _ := newService(t, settingsPath(t), false)
	svc.SetManagedOllama(m)
	on := true
	require.NoError(t, svc.Save(llmlocal.SaveInput{UseManagedOllama: &on, APIKey: "a-key-of-another-server"}))
	return svc
}

// Before a generation the Ollama of mam is not stopped: when the switch is on its models are unloaded through its own address (and the key of the other server is not sent); when it is off, nothing is touched.
func TestBeforeGeneration_UnloadsTheModelsOfTheManagedOllamaOnlyWhenTheSwitchIsOn(t *testing.T) {
	o := newOllama(t, "gemma4:12b")
	m := &managedOllama{url: o.URL, state: llmlocal.ManagedRunning}
	svc := useOllama(t, m)

	require.NoError(t, svc.BeforeGeneration(context.Background()))
	assert.Empty(t, o.unloaded, "the switch is off")

	on := true
	require.NoError(t, svc.Save(llmlocal.SaveInput{Unload: &on}))
	require.NoError(t, svc.BeforeGeneration(context.Background()))

	assert.Equal(t, []string{"gemma4:12b"}, o.unloaded)
	assert.Zero(t, m.stopped, "the server keeps running")
	for _, a := range o.auth {
		assert.Empty(t, a, "no Authorization header: the server has no authentication")
	}
}

// A stopped Ollama holds nothing: it is not asked and not started.
func TestBeforeGeneration_AStoppedManagedOllamaIsLeftAlone(t *testing.T) {
	o := newOllama(t, "gemma4:12b")
	m := &managedOllama{url: o.URL, state: llmlocal.ManagedStopped}
	svc := useOllama(t, m)
	on := true
	require.NoError(t, svc.Save(llmlocal.SaveInput{Unload: &on}))

	require.NoError(t, svc.BeforeGeneration(context.Background()))

	assert.Empty(t, o.unloaded)
	assert.Empty(t, o.auth, "no request at all")
}

// The model list of "ローカル LLM" is every model the managed Ollama holds, listed while it is stopped too; not while it is not installed.
func TestManagedModels_AreTheModelsOfTheManagedOllama(t *testing.T) {
	m := &managedOllama{state: llmlocal.ManagedStopped, models: []string{"gemma4:12b", "qwen3.8:27b"}}
	svc := useOllama(t, m)

	models, managed := svc.ManagedModels(context.Background())

	assert.True(t, managed)
	assert.Equal(t, []string{"gemma4:12b", "qwen3.8:27b"}, models)
	m.state = llmlocal.ManagedNotInstalled
	models, managed = svc.ManagedModels(context.Background())
	assert.True(t, managed)
	assert.Empty(t, models)
}
