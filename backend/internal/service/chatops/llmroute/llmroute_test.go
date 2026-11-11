package llmroute

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

func target(provider, model string) llmassign.Target {
	return llmassign.Target{Provider: provider, Model: model}
}

// fake is a llmassign.Service and an llmmodels.Service in memory.
type fake struct {
	view    llmassign.View
	saved   []map[string]llmassign.Route
	saveErr error
	lists   map[string]llmmodels.List
	avail   map[string]modelfilter.Availability
}

func newFake() *fake {
	entry := func(agent, label string, primary, fallback llmassign.Target) llmassign.Entry {
		r := llmassign.Route{Primary: primary, Fallback: fallback}
		return llmassign.Entry{Agent: agent, Label: label, Route: r, Default: r}
	}
	api := func(provider string, ids ...string) llmmodels.List {
		l := llmmodels.List{Provider: provider, Source: llmmodels.SourceAPI, Availability: modelfilter.Usable()}
		for _, id := range ids {
			l.Models = append(l.Models, llmmodels.Model{ID: id})
		}
		return l
	}
	return &fake{
		view: llmassign.View{
			Providers: []string{"anthropic", "openai", "google", "chatgpt-subscription", "chatgpt-codex", "local"}, Live: true,
			Entries: []llmassign.Entry{
				entry("director", "Director（Scene / Shot の計画）", target("anthropic", "claude-sonnet-4-5"), target("openai", "gpt-5")),
				entry("producer", "Producer（コンセプト）", target("openai", "gpt-5"), target("google", "gemini-2.5-pro")),
				entry("art_director", "Art Director（Reference Pack）", target("google", "gemini-2.5-pro"), target("openai", "gpt-5")),
				{Agent: "final_qc", Label: "Final QC（最終検査）", Unused: true, Route: llmassign.Route{Primary: target("openai", "gpt-5"), Fallback: target("openai", "gpt-5")}},
			},
		},
		lists: map[string]llmmodels.List{
			"anthropic": api("anthropic", "claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5"),
			"openai":    api("openai", "gpt-5", "gpt-5-mini", "o3"),
			"google":    api("google", "gemini-2.5-pro"),
		},
		avail: map[string]modelfilter.Availability{},
	}
}

func (f *fake) handler() *Handler {
	return New(Deps{
		View: func() llmassign.View { return f.view },
		Save: func(routes map[string]llmassign.Route) error {
			if f.saveErr != nil {
				return f.saveErr
			}
			f.saved = append(f.saved, maps.Clone(routes))
			return nil
		},
		List: func(_ context.Context, provider string) (llmmodels.List, error) {
			l, ok := f.lists[provider]
			if !ok {
				return llmmodels.List{}, llmmodels.ErrUnknownProvider
			}
			return l, nil
		},
		Availability: func(provider string) (modelfilter.Availability, error) {
			if a, ok := f.avail[provider]; ok {
				return a, nil
			}
			return modelfilter.Usable(), nil
		},
	})
}

func opOf(t *testing.T, a Args) commandinterpreter.Operation {
	t.Helper()
	op, err := commandinterpreter.NewOperation(OpSetLLMAssignment, a)
	require.NoError(t, err)
	return op
}

func ask(t *testing.T, f *fake, a Args) (chatops.Card, error) {
	t.Helper()
	return f.handler().Question(context.Background(), domain.Project{}, opOf(t, a))
}

func refusal(t *testing.T, err error) string {
	t.Helper()
	var r *chatops.Refusal
	require.ErrorAs(t, err, &r)
	return r.Message
}

// "Director の LLM を Claude Opus にして": the provider is the one the name stands for, the model is the one of its list; the card shows the route before and after and says it is
// global and at once; nothing is saved until Apply, which saves only this Agent, with the other half of its route as saved.
func TestPrimary_CardAsksFirstAndApplySavesOnlyThatAgent(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Agent: "director", Model: "Claude Opus"})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "Director（Scene / Shot の計画） のプライマリの LLM を変えますね？")
	assert.Contains(t, card.Text, "いま: プライマリ anthropic / claude-sonnet-4-5 / 代替 openai / gpt-5")
	assert.Contains(t, card.Text, "変更後: プライマリ anthropic / claude-opus-4-5 / 代替 openai / gpt-5")
	assert.Contains(t, card.Text, "全プロジェクトに効きます")
	assert.Contains(t, card.Text, "すぐ使われます（再起動は要りません）")
	assert.NotContains(t, card.Text, "注意")
	assert.Empty(t, f.saved, "nothing is saved before the user accepts")
	require.NotNil(t, card.Op)

	res, err := f.handler().Apply(context.Background(), domain.Project{}, *card.Op)

	require.NoError(t, err)
	assert.Contains(t, res.Text, "プライマリを anthropic / claude-opus-4-5 にしました")
	assert.Contains(t, res.Text, "全プロジェクト")
	require.Len(t, f.saved, 1)
	assert.Equal(t, map[string]llmassign.Route{"director": {Primary: target("anthropic", "claude-opus-4-5"), Fallback: target("openai", "gpt-5")}}, f.saved[0], "only the named Agent is saved")
}

func TestFallback_AModelNameThatFitsSeveralIsAskedAsButtons(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Agent: "producer", Role: "fallback", Model: "GPT"})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "「GPT」に当てはまるものが複数あります")
	require.Len(t, card.Choices, 2)
	assert.Equal(t, "gpt-5", card.Choices[0].Label)
	assert.Equal(t, "gpt-5-mini", card.Choices[1].Label)
	assert.Empty(t, f.saved)

	res, err := f.handler().Apply(context.Background(), domain.Project{}, card.Choices[1].Op)

	require.NoError(t, err)
	assert.Contains(t, res.Text, "代替を openai / gpt-5-mini にしました")
	assert.Equal(t, map[string]llmassign.Route{"producer": {Primary: target("openai", "gpt-5"), Fallback: target("openai", "gpt-5-mini")}}, f.saved[0], "the primary stays")
}

func TestTheProviderOfTheRouteStaysWhenTheModelNamesNone(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Agent: "director", Role: "fallback", Model: "o3"})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "変更後: プライマリ anthropic / claude-sonnet-4-5 / 代替 openai / o3")
	_, err = ask(t, f, Args{Agent: "producer", Role: "fallback", Model: "gemini-2.5-pro"})
	assert.ErrorIs(t, err, chatops.ErrNoChange, "the name stands for google, which the route already has")
}

func TestAgentResolution(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Agent: "Director", Model: "gpt-5-mini", Provider: "openai"})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "Director（Scene", "the exact name is Director, not Art Director")

	card, err = ask(t, f, Args{Agent: "Art Director", Model: "gpt-5-mini", Provider: "openai"})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "Art Director（Reference Pack）")

	card, err = ask(t, f, Args{Agent: "dir", Model: "gpt-5-mini", Provider: "openai"})
	require.NoError(t, err)
	require.Len(t, card.Choices, 2, "dir fits Director and Art Director")
	assert.Equal(t, "Director（Scene / Shot の計画）", card.Choices[0].Label)
	var picked Args
	require.NoError(t, card.Choices[1].Op.DecodeArgs(&picked))
	assert.Equal(t, "art_director", picked.Agent)
	assert.Equal(t, "gpt-5-mini", picked.Model)

	_, err = ask(t, f, Args{Agent: "ghost", Model: "gpt-5"})
	assert.Contains(t, refusal(t, err), "Producer（コンセプト）")
	_, err = ask(t, f, Args{Model: "gpt-5"})
	assert.Contains(t, refusal(t, err), "どの Agent の LLM ですか？")
	assert.Empty(t, f.saved)
}

func TestAModelThatIsInNoListIsAWarningNotARefusal(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Agent: "director", Provider: "anthropic", Model: "claude-opus-9"})

	require.NoError(t, err, "as the MCP tool: saved with a warning")
	assert.Contains(t, card.Text, "変更後: プライマリ anthropic / claude-opus-9")
	assert.Contains(t, card.Text, "「claude-opus-9」は anthropic の一覧にありません")
	res, err := f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "注意: 「claude-opus-9」は anthropic の一覧にありません")
	assert.Equal(t, "claude-opus-9", f.saved[0]["director"].Primary.Model)

	f.lists["anthropic"] = llmmodels.List{Provider: "anthropic", Source: llmmodels.SourceDefault, Fallback: llmmodels.FallbackNoKey, Models: []llmmodels.Model{{ID: "claude-opus-4-5"}}}
	card, err = ask(t, f, Args{Agent: "director", Provider: "anthropic", Model: "claude-opus-9"})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "mam の既定候補にありません")
}

func TestAWordThatIsNoModelNameIsRefusedWithTheCandidates(t *testing.T) {
	f := newFake()

	_, err := ask(t, f, Args{Agent: "director", Provider: "anthropic", Model: "とても賢いやつ"})
	assert.Contains(t, refusal(t, err), "「とても賢いやつ」はモデル名として読み取れません")
	_, err = ask(t, f, Args{Agent: "director", Provider: "anthropic", Model: "very smart one"})
	msg := refusal(t, err)
	assert.Contains(t, msg, "空白を含まない半角の英数字")
	assert.Contains(t, msg, "claude-opus-4-5")
	_, err = ask(t, f, Args{Agent: "director", Provider: "anthropic"})
	assert.Contains(t, refusal(t, err), "モデル名を添えて")
	_, err = ask(t, f, Args{Agent: "director", Provider: "cohere", Model: "x"})
	assert.Contains(t, refusal(t, err), "選べるプロバイダー")
	assert.Empty(t, f.saved)
}

func TestAProviderThatCannotBeUsedNowIsAWarning(t *testing.T) {
	f := newFake()
	f.avail["anthropic"] = modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません", "/settings/api-keys")

	card, err := ask(t, f, Args{Agent: "producer", Provider: "anthropic", Model: "claude-opus-4-5"})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "注意: anthropic は今使えません: API キーが登録されていません")
	assert.NotContains(t, card.Text, "sk-", "no key is ever shown")
}

func TestARouteThatIsAlreadyTheSameAsksNothing(t *testing.T) {
	f := newFake()

	_, err := ask(t, f, Args{Agent: "director", Provider: "anthropic", Model: "claude-sonnet-4-5"})
	assert.ErrorIs(t, err, chatops.ErrNoChange)

	res, err := f.handler().Apply(context.Background(), domain.Project{}, opOf(t, Args{Agent: "director", Provider: "anthropic", Model: "claude-sonnet-4-5"}))
	require.NoError(t, err)
	assert.Contains(t, res.Text, "すでにその内容です")
	assert.Empty(t, f.saved)
}

func TestWhatTheServiceRefusesIsToldAndNothingIsSaved(t *testing.T) {
	f := newFake()
	a := Args{Agent: "director", Provider: "anthropic", Model: "claude-opus-4-5"}

	f.view.Pinned = true
	_, err := ask(t, f, a)
	assert.Contains(t, refusal(t, err), "--llm-config")
	f.view.Pinned = false

	f.view.Problem = "invalid character"
	_, err = ask(t, f, a)
	assert.Contains(t, refusal(t, err), "読み込めない")
	f.view.Problem = ""

	card, err := ask(t, f, a)
	require.NoError(t, err)
	f.saveErr = &llmassign.InvalidError{Message: "Director（Scene / Shot の計画） のプライマリ: モデル名が長すぎます"}
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	assert.Contains(t, refusal(t, err), "モデル名が長すぎます")
	f.saveErr = llmassign.ErrPinned
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	assert.Contains(t, refusal(t, err), "--llm-config")
	f.saveErr = errors.New("disk")
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	var r *chatops.Refusal
	assert.False(t, errors.As(err, &r))
	assert.Empty(t, f.saved)
}

func TestWithoutALiveClientTheCardSaysItWaitsForARestartAndTheAnswerSaysWhatPends(t *testing.T) {
	f := newFake()
	f.view.Live = false
	f.view.Entries = f.view.Entries[:3]

	_, err := ask(t, f, Args{Agent: "final_qc", Model: "gpt-5-mini", Provider: "openai"})
	assert.Contains(t, refusal(t, err), "どの Agent", "final_qc is not among the entries of this view")

	card, err := ask(t, f, Args{Agent: "director", Provider: "anthropic", Model: "claude-opus-4-5"})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "次にアプリを起動したときから使われます")
	assert.NotContains(t, card.Text, "すぐ使われます")

	f.view.Live, f.view.Pending = true, []string{"timeout"}
	res, err := f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "再起動するまで効かない設定があります: timeout")
}

func TestAnAgentTheCodeDoesNotUseIsSaidSo(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Agent: "final_qc", Provider: "openai", Model: "gpt-5-mini"})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "今のコードでは使われません")
}
