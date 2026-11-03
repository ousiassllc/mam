package failmsg

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// providerNames are the vendors as the settings screen calls them (the API keys section).
var providerNames = map[llm.Provider]string{
	llm.ProviderAnthropic: "Anthropic",
	llm.ProviderOpenAI:    "OpenAI",
	llm.ProviderGoogle:    "Google",
}

// SubscriptionAdvice is the sentence that says how to run the Agents on a ChatGPT plan instead of an API key. The settings
// screen (/settings/llm) is where the provider is chosen; the failure notice of the chat links to it.
const SubscriptionAdvice = "ChatGPT のサブスクで動かすなら、先に「API キーとログイン」で ChatGPT にログインし、「テキスト生成（LLM）」でこの Agent のプロバイダーを chatgpt-subscription（組織のアカウントは chatgpt-codex）にしてください"

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

// NoKeyMessage is what the user reads when err is a failure for want of an API key (errors.Is(err, llm.ErrNoAPIKey)): which Agent
// failed and with which provider and model, which keys are missing, how to run on a ChatGPT plan instead, and a saved setting
// that waits for a restart. The log keeps the raw error.
func NoKeyMessage(err error) string {
	var route *llm.RouteError
	if !errors.As(err, &route) {
		return "API キーが設定されていません（設定画面の「API キーとログイン」で、使うプロバイダーのキー（LLM は Anthropic / OpenAI / Google）を登録してください。LLM は、" + SubscriptionAdvice + "）"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "LLM の API キーが設定されていません。失敗した Agent は %s です。", Label(route.Agent))
	var tried, missing []string
	for _, f := range route.Tried {
		why := "呼び出しに失敗"
		switch {
		case errors.Is(f.Err, llm.ErrNoAPIKey):
			why = "API キー未設定"
			if name := providerName(f.Target.Provider); !slices.Contains(missing, name) {
				missing = append(missing, name)
			}
		case errors.Is(f.Err, llm.ErrCircuitOpen):
			why = "一時的に利用不可"
		}
		tried = append(tried, fmt.Sprintf("%s / %s（%s）", f.Target.Provider, f.Target.Model, why))
	}
	fmt.Fprintf(&b, "試したプロバイダーとモデル: %s。", strings.Join(tried, "、"))
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\n対処: 設定画面の「API キーとログイン」で %s の API キーを登録してください。", strings.Join(missing, "・"))
	}
	fmt.Fprintf(&b, "\n%s。", SubscriptionAdvice)
	if len(route.RestartPending) > 0 {
		fmt.Fprintf(&b, "\n保存した設定のうち、%s は再起動すると反映されます（設定画面の「再起動して反映」）。", strings.Join(route.RestartPending, "・"))
	}
	return b.String()
}

func providerName(p llm.Provider) string {
	if n, ok := providerNames[p]; ok {
		return n
	}
	return string(p)
}
