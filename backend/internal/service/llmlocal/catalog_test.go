package llmlocal_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// catalogServer is a managed server that lists models for the picker.
type catalogServer struct {
	stubManaged
	models []llmlocal.ManagedModel
}

func (c *catalogServer) Info(context.Context) llmlocal.ManagedInfo {
	return llmlocal.ManagedInfo{State: llmlocal.ManagedStopped, Catalog: c.models}
}

// Both servers of mam are listed whichever one the calls go to: the one in use as it says itself, the other with "使うサーバー" as the reason; a model that already has a reason keeps it.
func TestManagedCatalogListsBothServersAndSaysWhichUseMakesAModelUsable(t *testing.T) {
	svc, _ := newService(t, settingsPath(t), false)
	llama := &catalogServer{models: []llmlocal.ManagedModel{
		{ID: "gemma-4-12b-it", Name: "Gemma", License: modellicense.Info{Commercial: modellicense.Yes}},
		{ID: "qwen-abl", Name: "Qwen 非検閲", Uncensored: true, Reason: "今の llama.cpp のモデルは「Gemma」です"},
	}}
	ollama := &catalogServer{models: []llmlocal.ManagedModel{{ID: "huihui_ai/gemma-4-abliterated:12b", Name: "Gemma 非検閲", Uncensored: true}}}
	svc.SetManaged(llama)
	svc.SetManagedOllama(ollama)

	byID := func() map[string]llmlocal.ManagedModel {
		out := map[string]llmlocal.ManagedModel{}
		for _, m := range svc.ManagedCatalog(context.Background()) {
			out[m.ID] = m
		}
		return out
	}

	got := byID() // the calls go to an address of the settings
	require.Len(t, got, 3)
	assert.Equal(t, "この PC の llama.cpp", got["gemma-4-12b-it"].Server)
	assert.Equal(t, "Ollama", got["huihui_ai/gemma-4-abliterated:12b"].Server)
	assert.Contains(t, got["gemma-4-12b-it"].Reason, "「使うサーバー」は「別のサーバー」です")
	assert.Contains(t, got["huihui_ai/gemma-4-abliterated:12b"].Reason, "「Ollama」を選ぶと使えます")
	assert.Equal(t, "今の llama.cpp のモデルは「Gemma」です", got["qwen-abl"].Reason, "its own reason comes first")

	on := true
	require.NoError(t, svc.Save(llmlocal.SaveInput{UseManagedOllama: &on}))
	got = byID()
	assert.Empty(t, got["huihui_ai/gemma-4-abliterated:12b"].Reason, "Ollama is in use and the model is fetched")
	assert.Contains(t, got["gemma-4-12b-it"].Reason, "「使うサーバー」は「Ollama」です")
}

// An app without a managed server lists none.
func TestManagedCatalogIsEmptyWithoutManagedServers(t *testing.T) {
	svc, _ := newService(t, settingsPath(t), false)

	assert.Empty(t, svc.ManagedCatalog(context.Background()))
}
