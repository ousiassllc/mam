package llmmodels_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// #1013: a provider can be used when an API key is saved or the subscription is signed in; the others say why, and where to fix it.
func TestAvailability_NeedsAKeyOrASignIn(t *testing.T) {
	v := newVendor(t)
	in := &signin{account: ""}
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t, "openai", secret), ChatGPT: in, BaseURLs: map[llm.Provider]string{llm.ProviderOpenAI: v.URL}})

	a, err := svc.Availability("openai")
	require.NoError(t, err)
	assert.Equal(t, modelfilter.Usable(), a, "a saved key")

	a, err = svc.Availability("anthropic")
	require.NoError(t, err)
	assert.False(t, a.Available)
	assert.Equal(t, modelfilter.ReasonNoKey, a.Reason)
	assert.Contains(t, a.Message, "API キー")
	assert.Equal(t, "/settings/api-keys", a.Route)
	assert.EqualValues(t, 0, v.asks.Load(), "the availability reads the key store only: no vendor is asked")

	a, err = svc.Availability("chatgpt-subscription")
	require.NoError(t, err)
	assert.False(t, a.Available)
	assert.Equal(t, modelfilter.ReasonNotSignedIn, a.Reason)
	assert.Contains(t, a.Message, "ログイン")
	assert.Equal(t, "/settings/api-keys", a.Route, "the ChatGPT sign-in is on the same settings category")

	in.account = "a@example.com"
	a, _ = svc.Availability("chatgpt-subscription")
	assert.True(t, a.Available, "signed in")

	_, err = svc.Availability("nope")
	assert.ErrorIs(t, err, llmmodels.ErrUnknownProvider)
}

func TestList_CarriesTheAvailabilityOfItsProvider(t *testing.T) {
	v := newVendor(t)
	v.status.Store(500)
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t, "openai", secret), BaseURLs: map[llm.Provider]string{llm.ProviderOpenAI: v.URL}})
	got, err := svc.List(context.Background(), "openai")
	require.NoError(t, err)
	assert.Equal(t, llmmodels.FallbackFailed, got.Fallback, "the vendor did not answer")
	assert.True(t, got.Availability.Available, "a provider that did not answer still has its key: its models are offered")

	got, err = svc.List(context.Background(), "anthropic")
	require.NoError(t, err)
	assert.False(t, got.Availability.Available)
	assert.Equal(t, modelfilter.ReasonNoKey, got.Availability.Reason)
}

// The picker of the LLM assignment starts with the providers that cannot be used left out, and "使えないものも表示" lists them with the reason.
func TestCandidates_HideWhatCannotBeUsedUntilAsked(t *testing.T) {
	v := newVendor(t)
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t, "openai", secret), BaseURLs: map[llm.Provider]string{llm.ProviderOpenAI: v.URL}})

	shown, err := svc.Candidates(context.Background(), modelfilter.Default())
	require.NoError(t, err)
	require.NotEmpty(t, shown)
	for _, c := range shown {
		assert.Equal(t, "openai", c.Provider, "only the provider with a key is offered")
		assert.True(t, c.Availability.Available)
	}
	assert.Equal(t, "gpt-live-1", shown[0].Model.ID)

	everything, err := svc.Candidates(context.Background(), modelfilter.Condition{})
	require.NoError(t, err)
	providers := map[string]bool{}
	for _, c := range everything {
		providers[c.Provider] = true
		if c.Provider != "openai" {
			assert.False(t, c.Availability.Available, c.Provider)
			assert.NotEmpty(t, c.Availability.Message, c.Provider)
			assert.NotEmpty(t, c.Availability.Route, c.Provider)
			assert.Equal(t, c.Availability.Message, c.Item().Reason)
		}
	}
	assert.Len(t, providers, len(llm.Providers()), "every provider is listed when the unusable ones are asked for")
	assert.EqualValues(t, 1, v.asks.Load(), "the one provider with a key is asked once; the others use mam's candidates")

	anthropic, err := svc.Candidates(context.Background(), modelfilter.Condition{Providers: []string{"anthropic"}, Text: "claude"})
	require.NoError(t, err)
	assert.NotEmpty(t, anthropic, "the provider and the text search narrow the list too")
	for _, c := range anthropic {
		assert.Equal(t, "anthropic", c.Provider)
		assert.Contains(t, c.Item().Kinds, modelfilter.KindLLM)
	}
}

// A saved assignment that points at a provider that cannot be used is a warning (the save is not refused: a key may come later).
func TestWarnings_AssignmentsAtUnusableProviders(t *testing.T) {
	svc := llmmodels.New(llmmodels.Options{Keys: keys(t, "openai", secret)})
	got := svc.Warnings([]llmmodels.Target{
		{Agent: "prompt_planner", Role: "primary", Provider: "openai", Model: "gpt-x"},
		{Agent: "prompt_planner", Role: "fallback", Provider: "anthropic", Model: "claude-x"},
		{Agent: "judge", Role: "primary", Provider: "chatgpt-subscription", Model: "m"},
		{Agent: "judge", Role: "fallback", Provider: "no-such-provider", Model: "m"},
	})
	require.Len(t, got, 2, "the provider with a key is fine and the unknown one was refused by the save already")
	assert.Equal(t, "anthropic", got[0].Provider)
	assert.Equal(t, "fallback", got[0].Role)
	assert.Contains(t, got[0].Message, "prompt_planner")
	assert.Contains(t, got[0].Message, "代替")
	assert.Contains(t, got[0].Message, "API キーが登録されていません")
	assert.Equal(t, "/settings/api-keys", got[0].Availability.Route)
	assert.Equal(t, "chatgpt-subscription", got[1].Provider)
	assert.Contains(t, got[1].Message, "ログイン")
	assert.Empty(t, svc.Warnings(nil))
}
