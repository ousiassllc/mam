package llmroute

import (
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// pickAgents finds the Agent the text names among entries: the one called so, else every one whose name holds the text.
func pickAgents(entries []llmassign.Entry, text string) []llmassign.Entry {
	q := nlu.Norm(text)
	if q == "" {
		return nil
	}
	var holding []llmassign.Entry
	for _, e := range entries {
		keys := nlu.AgentKeys(e.Agent, e.Label)
		if slices.Contains(keys, q) {
			return []llmassign.Entry{e}
		}
		if slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(k, q) }) {
			holding = append(holding, e)
		}
	}
	return holding
}

// providerNames are the words that mean a provider, beside its ID. Every provider of llm.Providers is here (TestProviderNamesCoverEveryProvider).
var providerNames = []struct {
	id    string
	names []string
}{
	{string(llm.ProviderAnthropic), []string{"anthropic", "claude", "クロード"}},
	{string(llm.ProviderOpenAI), []string{"openai", "gpt", "オープンエーアイ"}},
	{string(llm.ProviderGoogle), []string{"google", "gemini", "グーグル", "ジェミニ"}},
	{string(llm.ProviderChatGPTSubscription), []string{"chatgpt", "chatgptsubscription", "chatgptサブスク"}},
	{string(llm.ProviderChatGPTCodex), []string{"codex", "chatgptcodex", "コーデックス"}},
	// gptoss: the open-weight GPT models are run locally, they are not on the OpenAI API, so "gpt" in their name must not choose OpenAI
	{string(llm.ProviderLocal), []string{"local", "ローカル", "ollama", "lmstudio", "llamacpp", "gptoss"}},
}

// providerOf finds the provider the text names: its ID or one of its names. The longest name found wins ("ChatGPT Codex" is not "ChatGPT"); false when none is named or two
// providers tie.
func providerOf(text string) (string, bool) {
	n := nlu.Norm(text)
	if n == "" {
		return "", false
	}
	var (
		best    string
		bestLen int
		tie     bool
	)
	for _, p := range providerNames {
		for _, name := range append([]string{nlu.Norm(p.id)}, p.names...) {
			name = nlu.Norm(name)
			if !strings.Contains(n, name) {
				continue
			}
			switch {
			case len(name) > bestLen:
				best, bestLen, tie = p.id, len(name), false
			case len(name) == bestLen && p.id != best:
				tie = true
			}
		}
	}
	return best, bestLen > 0 && !tie
}

// pickModels finds the models of the list the text names: the one called so, else every one that holds every word of the text ("Claude Opus" is claude-opus-4-5).
func pickModels(l llmmodels.List, text string) []llmmodels.Model {
	q := nlu.Norm(text)
	if q == "" {
		return nil
	}
	for _, m := range l.Models {
		if nlu.Norm(m.ID) == q || (m.Name != "" && nlu.Norm(m.Name) == q) {
			return []llmmodels.Model{m}
		}
	}
	words := strings.Fields(text)
	var holding []llmmodels.Model
	for _, m := range l.Models {
		id, name := nlu.Norm(m.ID), nlu.Norm(m.Name)
		if slices.ContainsFunc(words, func(w string) bool {
			return nlu.Norm(w) != "" && !strings.Contains(id, nlu.Norm(w)) && !strings.Contains(name, nlu.Norm(w))
		}) {
			continue
		}
		holding = append(holding, m)
	}
	return holding
}

// modelLabel is how a candidate is shown: its ID, with its display name when it has another one.
func modelLabel(m llmmodels.Model) string {
	if m.Name != "" && m.Name != m.ID {
		return m.ID + "（" + m.Name + "）"
	}
	return m.ID
}
