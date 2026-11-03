package llmmodels_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// While the user has chosen mam's own llama.cpp, the list of "local" is its one model (and the server of the settings is not asked); choosing it, or
// another model, is read again, and a stopped server lists its model too.
func TestLocal_TheManagedServersModelIsTheListWhileItIsChosen(t *testing.T) {
	s := newLocalServer(t, ollamaModels)
	chosen, model := false, "gemma-4-12b-it"
	svc := llmmodels.New(llmmodels.Options{
		Keys: keys(t), LocalBaseURL: func() string { return s.URL },
		LocalManaged: func(context.Context) ([]string, bool) { return []string{model}, chosen },
	})

	got, err := svc.List(context.Background(), "local")
	require.NoError(t, err)
	assert.Equal(t, []string{"gemma-4-12b", "qwen3.8:27b"}, ids(got), "not chosen: the address of the settings")

	chosen = true
	got, err = svc.List(context.Background(), "local")
	require.NoError(t, err)
	assert.Equal(t, []string{"gemma-4-12b-it"}, ids(got))
	assert.Equal(t, llmmodels.SourceAPI, got.Source)
	assert.EqualValues(t, 1, s.asks.Load(), "the server of the settings was not asked again")

	model = "qwen3.8-27b"
	got, err = svc.List(context.Background(), "local")
	require.NoError(t, err)
	assert.Equal(t, []string{"qwen3.8-27b"}, ids(got), "another model: read again")

	chosen = false
	got, _ = svc.List(context.Background(), "local")
	assert.Equal(t, []string{"gemma-4-12b", "qwen3.8:27b"}, ids(got), "not chosen any more: the address again")
}

// A managed server with no model to offer (not supported, not installed) lists nothing, and it is not an error: the card says why.
func TestLocal_AManagedServerWithoutAModelListsNothing(t *testing.T) {
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalManaged: func(context.Context) ([]string, bool) { return nil, true }})

	got, err := svc.List(context.Background(), "local")

	require.NoError(t, err)
	assert.Empty(t, got.Models)
}
