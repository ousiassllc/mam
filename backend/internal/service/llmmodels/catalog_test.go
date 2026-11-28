package llmmodels_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

func managedCatalog() []llmlocal.ManagedModel {
	yes := modellicense.Info{Commercial: modellicense.Yes, LicenseName: "Apache License 2.0"}
	unknown := modellicense.Info{Commercial: modellicense.Unknown, LicenseName: "ライセンス不明", Notice: "確かめてください"}
	pcCannot := "Linux では llama.cpp の自動導入に対応していません"
	return []llmlocal.ManagedModel{
		{ID: "gemma-4-12b-it", Name: "Gemma 4 12B-it", Server: "この PC の llama.cpp", License: yes, Reason: pcCannot},
		{ID: "gemma-4-12b-it-abliterated", Name: "Gemma 4 12B-it 非検閲", Server: "この PC の llama.cpp", Uncensored: true, License: yes, Reason: pcCannot},
		{ID: "deepseek-r1-14b-abliterated-v2", Name: "DeepSeek 非検閲", Server: "この PC の llama.cpp", Uncensored: true, License: unknown, Reason: pcCannot},
		{ID: "huihui_ai/gemma-4-abliterated:12b", Name: "Gemma 4 12B 非検閲", Server: "Ollama", Uncensored: true, License: yes},
	}
}

func localOnly(c modelfilter.Condition) modelfilter.Condition {
	c.Providers = []string{"local"}
	return c
}

func catalogOf(models []llmlocal.ManagedModel) func(context.Context) []llmlocal.ManagedModel {
	return func(context.Context) []llmlocal.ManagedModel { return models }
}

func modelIDs(cs []llmmodels.Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Model.ID)
	}
	return out
}

// The reported case: on a PC that cannot run llama.cpp the uncensored models used to be missing from the picker with no word. They are listed now: hidden by "選べるものだけ" at first, and
// with "使えないものも表示" each is there with the reason of the PC and the way to the page that fixes it. The ones that can be chosen (a fetched Ollama model) are shown at once.
func TestCandidates_TheModelsOfTheServersOfMamAreListedWithTheirReasons(t *testing.T) {
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: closedURL(t), LocalCatalog: catalogOf(managedCatalog())})

	shown, err := svc.Candidates(context.Background(), localOnly(modelfilter.Default()))
	require.NoError(t, err)
	assert.Equal(t, []string{"huihui_ai/gemma-4-abliterated:12b"}, modelIDs(shown), "only what can be chosen now")

	all, err := svc.Candidates(context.Background(), localOnly(modelfilter.Condition{Uncensored: modelfilter.UncensoredOnly}))
	require.NoError(t, err)
	require.Len(t, all, 3)
	byID := map[string]llmmodels.Candidate{}
	for _, c := range all {
		byID[c.Model.ID] = c
	}
	blocked := byID["gemma-4-12b-it-abliterated"]
	assert.False(t, blocked.Availability.Available)
	assert.Equal(t, "Linux では llama.cpp の自動導入に対応していません", blocked.Model.Unavailable.Message)
	assert.Equal(t, "/model-manager/local-llm", blocked.Model.Unavailable.Route)
	assert.Equal(t, "Gemma 4 12B-it 非検閲", blocked.Item().Name)
	assert.True(t, blocked.Item().Uncensored)
	assert.True(t, blocked.Item().Adult, "an uncensored model is an adult one, as in the runtimes' lists")
	assert.Equal(t, modelfilter.OriginLocal, blocked.Item().Origin)
	assert.False(t, blocked.Item().Available)
	assert.Equal(t, modellicense.Unknown, byID["deepseek-r1-14b-abliterated-v2"].Item().Commercial)

	censored, err := svc.Candidates(context.Background(), localOnly(modelfilter.Condition{Uncensored: modelfilter.CensoredOnly}))
	require.NoError(t, err)
	assert.Equal(t, []string{"gemma-4-12b-it"}, modelIDs(censored))
}

// The setting 「商用OKのモデルだけを使う」 asks for "商用OKのみ": a model with no license (the DeepSeek derivative) is out of that list, the others stay; "商用OK + 不明" brings it back.
func TestCandidates_ACommercialChoiceNarrowsTheModelsOfMamByTheirLicense(t *testing.T) {
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: closedURL(t), LocalCatalog: catalogOf(managedCatalog())})
	unc := modelfilter.Condition{Uncensored: modelfilter.UncensoredOnly, Providers: []string{"local"}}

	unc.Commercial = "ok"
	ok, err := svc.Candidates(context.Background(), unc)
	require.NoError(t, err)
	assert.Len(t, ok, 2)

	unc.Commercial = "ok,unknown"
	both, err := svc.Candidates(context.Background(), unc)
	require.NoError(t, err)
	assert.Len(t, both, 3)
}

// A model the server that answers lists is chosen, and takes the name, the license and the flag of the catalog.
func TestCandidates_AListedModelTakesTheCatalogsFacts(t *testing.T) {
	s := newLocalServer(t, `{"object":"list","data":[{"id":"huihui_ai/gemma-4-abliterated:12b"}]}`)
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: func() string { return s.URL }, LocalCatalog: catalogOf(managedCatalog())})

	got, err := svc.Candidates(context.Background(), localOnly(modelfilter.Default()))

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "huihui_ai/gemma-4-abliterated:12b", got[0].Model.ID)
	assert.True(t, got[0].Model.Uncensored)
	assert.Equal(t, "Gemma 4 12B 非検閲", got[0].Item().Name)
	assert.True(t, got[0].Availability.Available)
}

// The shipped candidates stand in for a list that could not be read: the model of the same name in the catalog replaces one, with its reason, not the provider's "ローカルサーバーに接続できません".
func TestCandidates_ACatalogModelReplacesTheShippedCandidateOfTheSameNameWithItsReason(t *testing.T) {
	models := []llmlocal.ManagedModel{{ID: "gemma-4-12b", Name: "Gemma", Uncensored: true, Server: "Ollama", Reason: "まだ取得していません"}}
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: closedURL(t), LocalCatalog: catalogOf(models)})

	got, err := svc.Candidates(context.Background(), localOnly(modelfilter.Condition{Text: "gemma-4-12b"}))

	require.NoError(t, err)
	require.Len(t, got, 1, "the shipped candidate of that name is replaced, not duplicated")
	assert.Equal(t, "まだ取得していません", got[0].Availability.Message)
	assert.True(t, got[0].Model.Uncensored)
}

// Without a catalog nothing changes: the provider's own list is the whole list.
func TestCandidates_WithoutACatalogTheListIsTheProvidersOwn(t *testing.T) {
	s := newLocalServer(t, ollamaModels)
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t), LocalBaseURL: func() string { return s.URL }})

	got, err := svc.Candidates(context.Background(), localOnly(modelfilter.Default()))

	require.NoError(t, err)
	require.Len(t, got, 2, "the embedding model is not an LLM")
	for _, c := range got {
		assert.False(t, c.Model.Uncensored)
		assert.Nil(t, c.Model.Unavailable)
	}
}
