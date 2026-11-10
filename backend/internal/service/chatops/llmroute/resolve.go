package llmroute

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// norm is text as it is compared: lower case, with only letters and digits ("claude-opus-4" and "Claude Opus 4" meet).
func norm(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// agentKeys are the names an Agent is called by: its ID ("art_director"), its label ("Art Director（Reference Pack）") and the label without its note ("Art Director"),
// normalised.
func agentKeys(id, label string) []string {
	keys := []string{norm(id), norm(label)}
	if i := strings.IndexAny(label, "（("); i > 0 {
		keys = append(keys, norm(label[:i]))
	}
	return keys
}

// staticAgents are the Agents of the program with their labels, for what is read before any service is asked (Asked, detect).
func staticAgents() []llmassign.Entry {
	var out []llmassign.Entry
	for _, a := range llm.Agents() {
		out = append(out, llmassign.Entry{Agent: string(a), Label: failmsg.Label(a)})
	}
	return out
}

// agentIn finds the Agent a message names: the longest of its names the message holds ("Art Director" is not "Director"); "" for none.
func agentIn(message string) string {
	n := norm(message)
	best, bestLen := "", 0
	for _, e := range staticAgents() {
		for _, k := range agentKeys(e.Agent, e.Label) {
			if len(k) > bestLen && strings.Contains(n, k) {
				best, bestLen = e.Agent, len(k)
			}
		}
	}
	return best
}

func mentionsAgent(message string) bool { return agentIn(message) != "" }

// pickAgents finds the Agent the text names among entries: the one called so, else every one whose name holds the text.
func pickAgents(entries []llmassign.Entry, text string) []llmassign.Entry {
	q := norm(text)
	if q == "" {
		return nil
	}
	var holding []llmassign.Entry
	for _, e := range entries {
		keys := agentKeys(e.Agent, e.Label)
		if slices.Contains(keys, q) {
			return []llmassign.Entry{e}
		}
		if slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(k, q) }) {
			holding = append(holding, e)
		}
	}
	return holding
}

// providerNames are the words that mean a provider, beside its ID.
var providerNames = []struct {
	id    string
	names []string
}{
	{string(llm.ProviderAnthropic), []string{"anthropic", "claude", "クロード"}},
	{string(llm.ProviderOpenAI), []string{"openai", "gpt", "オープンエーアイ"}},
	{string(llm.ProviderGoogle), []string{"google", "gemini", "グーグル", "ジェミニ"}},
	{string(llm.ProviderChatGPTSubscription), []string{"chatgpt", "chatgptsubscription", "chatgptサブスク"}},
	{string(llm.ProviderChatGPTCodex), []string{"codex", "chatgptcodex", "コーデックス"}},
	{string(llm.ProviderLocal), []string{"local", "ローカル", "ollama", "lmstudio", "llamacpp"}},
}

// providerOf finds the provider the text names: its ID or one of its names. The longest name found wins ("ChatGPT Codex" is not "ChatGPT"); false when none is named or two
// providers tie.
func providerOf(text string) (string, bool) {
	n := norm(text)
	if n == "" {
		return "", false
	}
	var (
		best    string
		bestLen int
		tie     bool
	)
	for _, p := range providerNames {
		for _, name := range append([]string{norm(p.id)}, p.names...) {
			name = norm(name)
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
	q := norm(text)
	if q == "" {
		return nil
	}
	for _, m := range l.Models {
		if norm(m.ID) == q || (m.Name != "" && norm(m.Name) == q) {
			return []llmmodels.Model{m}
		}
	}
	words := strings.Fields(text)
	var holding []llmmodels.Model
	for _, m := range l.Models {
		id, name := norm(m.ID), norm(m.Name)
		if slices.ContainsFunc(words, func(w string) bool {
			return norm(w) != "" && !strings.Contains(id, norm(w)) && !strings.Contains(name, norm(w))
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

// few lists the first n names, and how many more there are.
func few(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, "、")
	}
	return strings.Join(names[:n], "、") + "、ほか " + strconv.Itoa(len(names)-n) + " 件"
}
