package failmsg

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// providerNames are the vendors as the settings screen calls them (the API keys section).
var providerNames = map[llm.Provider]string{
	llm.ProviderAnthropic: "Anthropic",
	llm.ProviderOpenAI:    "OpenAI",
	llm.ProviderGoogle:    "Google",
}

// SettingsSlug is the category of the settings screen that holds the LLM assignment (paths.SettingsLLM): the ref_id of the chat notice
// that links to it. The chat builds the address from the slug; it is never written into the message.
const SettingsSlug = "llm"

// RefType is the ref_type of the chat notice that tells err: the link to the LLM assignment ("テキスト生成の設定を開く") when err is
// a failure for want of an API key, none otherwise. Use it with RefID.
func RefType(err error) string {
	if errors.Is(err, llm.ErrNoAPIKey) {
		return domain.RefGlobalSettings
	}
	return ""
}

// Notice is the chat message of the AI that tells the failure err: the text, and for a missing key the link to the LLM assignment.
func Notice(err error, content string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content, RefType: RefType(err), RefID: RefID(err)}
}

// RefID is the ref_id that goes with RefType.
func RefID(err error) string {
	if errors.Is(err, llm.ErrNoAPIKey) {
		return SettingsSlug
	}
	return ""
}

// NoKeyError is the failure for want of an API key (errors.Is(err, llm.ErrNoAPIKey)) as the common error (#1010): which Agent failed
// and with which provider and model, which keys are missing, how to run on a ChatGPT plan instead (in the remedy), and a saved
// setting that waits for a restart. Its detail is the raw error, which the log keeps too.
func NoKeyError(err error) *apperr.Error {
	e := apperr.New(apperr.CodeLLMKeyMissing).Wrap(err).With("who", "{{llm_key_missing.any}}")
	var route *llm.RouteError
	if !errors.As(err, &route) {
		return e
	}
	var tried, missing []string
	for _, f := range route.Tried {
		why := "{{why.failed}}"
		switch {
		case errors.Is(f.Err, llm.ErrNoAPIKey):
			why = "{{why.no_key}}"
			if name := providerName(f.Target.Provider); !slices.Contains(missing, name) {
				missing = append(missing, name)
			}
		case errors.Is(f.Err, llm.ErrCircuitOpen):
			why = "{{why.circuit_open}}"
		}
		tried = append(tried, fmt.Sprintf("%s / %s%s", f.Target.Provider, f.Target.Model, why))
	}
	e = e.With("route", "{{llm_key_missing.route}}").With("agent", agentText(route.Agent)).With("tried", strings.Join(tried, "{{list.separator}}"))
	if len(missing) > 0 {
		e = e.With("who", "{{llm_key_missing.named}}").With("providers", strings.Join(missing, "{{list.join}}"))
	}
	if len(route.RestartPending) > 0 {
		e = e.With("restart", "{{restart.pending}}").With("items", strings.Join(route.RestartPending, "{{list.join}}"))
	}
	return e
}

// NoKeyMessage is what the user reads when err is a failure for want of an API key: the cause and the remedy of NoKeyError in
// Japanese. The log keeps the raw error.
func NoKeyMessage(err error) string {
	return NoKeyError(err).View(apperr.LangJa).Sentence(apperr.LangJa)
}

// agentText names the Agent in the language of the reader: the text of the catalog ("agent.<id>"), else the label.
func agentText(a llm.Agent) string {
	if key := "agent." + string(a); apperr.Text(apperr.LangJa, key, nil) != key {
		return "{{" + key + "}}"
	}
	return Label(a)
}

func providerName(p llm.Provider) string {
	if n, ok := providerNames[p]; ok {
		return n
	}
	return string(p)
}
