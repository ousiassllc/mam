package llm

import (
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// keysRoute is the screen where an API key is registered.
var keysRoute = paths.SettingsCategory(paths.SettingsAPIKeys)

// keyNames are the names of the providers that take an API key, as the LLM screen has them (the picker's provider names); a provider that is not here is its own ID.
var keyNames = map[string]string{"openai": "OpenAI", "anthropic": "Anthropic（Claude）", "google": "Google（Gemini）"}

func keyName(provider string) string {
	if n, ok := keyNames[provider]; ok {
		return n
	}
	return provider
}

// missingKey reports whether the provider cannot be used because its API key is not registered: the reason says no key, and the screen that fixes it is the API keys
// (a provider that is not signed in, or a local server that does not answer, is another cause).
func missingKey(a modelfilter.Availability) bool {
	return !a.Available && a.Reason == modelfilter.ReasonNoKey && a.Route == keysRoute
}

// availability is whether the provider can be used now; a provider mam does not know (the save refused it already) and a screen without the service are taken as usable.
func availability(models *llmmodels.Service, provider string) modelfilter.Availability {
	if models == nil {
		return modelfilter.Usable()
	}
	a, err := models.Availability(provider)
	if err != nil {
		return modelfilter.Usable()
	}
	return a
}

// chipOf is the chip of a target with the availability a: the word "キー未登録" while its API key is not registered, none otherwise (another cause is in the warnings).
func chipOf(a modelfilter.Availability) view.Chip {
	if !missingKey(a) {
		return view.Chip{}
	}
	return view.Chip{Label: "キー未登録", Title: a.Message}
}

// fallbackNote is what a row says when its primary cannot be used and its alternative can: the route runs on the alternative, which is not a failure.
func fallbackNote(primary, fallback modelfilter.Availability, fallbackModel string) string {
	if primary.Available || !fallback.Available {
		return ""
	}
	return "代替で動きます: 主モデルが使えないため、代替モデル " + fallbackModel + " を使います"
}

// shortName is the name of an Agent without the words in brackets after it ("Producer（コンセプト）" is "Producer").
func shortName(label string) string {
	name, _, _ := strings.Cut(label, "（")
	return name
}

// keyBanner counts the Agents that run (T: not the ones under "現在は使われません") and those that the missing API keys touch, and says for each missing key how many of them
// it puts back on their primary. An Agent is
//   - blocked when neither its primary nor its alternative can be used and one of them lacks its API key: nothing of it runs until a key is put in;
//   - covered when its primary lacks its API key and its alternative can be used: it runs on the alternative.
//
// A key's line names the blocked and covered Agents whose primary is of that provider and lacks its key (the key brings them back to the primary), so the lines add up to
// the blocked and covered Agents the title counts. An Agent whose primary runs is in no line, whatever its alternative lacks, and a key that brings no Agent back has no
// line. It is false when no Agent is blocked or covered.
func keyBanner(v llmassign.View, models *llmmodels.Service) (view.KeyBanner, bool) {
	var b view.KeyBanner
	var order []string // the providers in the order their first Agent is put back
	agents := map[string][]string{}
	for _, e := range v.Entries {
		if e.Unused {
			continue
		}
		b.Rows++
		primary, fallback := availability(models, e.Route.Primary.Provider), availability(models, e.Route.Fallback.Provider)
		noPrimary, noFallback := missingKey(primary), missingKey(fallback)
		switch {
		case !primary.Available && !fallback.Available && (noPrimary || noFallback):
			b.Blocked++
		case noPrimary && fallback.Available:
			b.Covered++
		default:
			continue
		}
		if !noPrimary {
			continue // its primary lacks something else than the key: no key brings it back
		}
		p := e.Route.Primary.Provider
		if _, ok := agents[p]; !ok {
			order = append(order, p)
		}
		agents[p] = append(agents[p], shortName(e.Label))
	}
	if b.Blocked == 0 && b.Covered == 0 {
		return view.KeyBanner{}, false
	}
	for _, p := range order {
		b.Shortages = append(b.Shortages, view.KeyShortage{Provider: keyName(p), Agents: agents[p]})
	}
	slices.SortStableFunc(b.Shortages, func(x, y view.KeyShortage) int { return len(y.Agents) - len(x.Agents) })
	b.Route = keysRoute
	return b, true
}
