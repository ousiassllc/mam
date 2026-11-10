// Package llmroute is the chat operation of the LLM assignment (#1140; spec/components/chat-settings-operations.md §3): the director asks for "Director の LLM を Claude Opus にして",
// "Producer の代替を GPT にして" and gets a card that shows the route of the Agent before and after; the answer saves it through the same llmassign.Service.Save as the settings
// screen and the MCP tool set_llm_assignment: only the named Agent and role change, the other half of its route stays. A model that is in no list of its provider is allowed, with a
// warning in the card, as in the MCP tool. The assignment is a setting of the whole app, which the card says.
package llmroute

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
)

// OpSetLLMAssignment changes the provider and model of one role of one Agent's route.
const OpSetLLMAssignment commandinterpreter.OpType = "set_llm_assignment"

// The roles of a route.
const (
	rolePrimary  = "primary"
	roleFallback = "fallback"
)

// Args of the operation. Agent is the Agent as the director said it (Question resolves its name or label), Role is primary (default) or fallback, Provider and Model are what the
// director said: the model name is resolved against the list of the provider. An empty Provider is the one the model's name stands for, else the route's own.
type Args struct {
	Agent    string `json:"agent,omitempty"`
	Role     string `json:"role,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

func init() {
	commandinterpreter.Register(OpSetLLMAssignment, commandinterpreter.Spec{
		Example: `{"type": "set_llm_assignment", "args": {"agent": "director", "role": "primary", "provider": "anthropic", "model": "claude-opus"}}`,
		Doc: `- "set_llm_assignment": the director wants the LLM (the provider and the model) an Agent calls changed ("Director の LLM を Claude Opus にして", "Producer の代替を GPT にして"). ` +
			`"args.agent" is one of ` + agentList() + ` (leave it out when the message names none). "args.role" is "primary" (default) or "fallback" (代替). "args.provider" is ` +
			`"anthropic", "openai", "google", "chatgpt-subscription", "chatgpt-codex" or "local" (leave it out when the message does not say). "args.model" is the model name as the director said it; ` +
			`do not complete it to a full identifier. It is not the image or video model and not the Project's own settings.` + "\n",
		Clean:   clean,
		Asked:   Asked,
		Detect:  detect,
		AnyMode: true, Confirm: true, Global: true, Setup: true,
	})
}

func agentList() string {
	ids := llmassign.Agents()
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, `"`+id+`"`)
	}
	return strings.Join(quoted, ", ")
}

// clean keeps the operation the model returned with its arguments in their normal form (the role as primary or fallback, the provider as an ID when the text names one).
func clean(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return commandinterpreter.Operation{}, false
	}
	a.Agent, a.Model = strings.TrimSpace(a.Agent), strings.TrimSpace(a.Model)
	a.Role = roleOf(a.Role)
	if id, ok := providerOf(a.Provider); ok {
		a.Provider = id
	} else {
		a.Provider = ""
	}
	out, err := commandinterpreter.NewOperation(OpSetLLMAssignment, a)
	return out, err == nil
}

// roleOf is primary unless the text says the alternative.
func roleOf(text string) string {
	n := norm(text)
	if strings.Contains(n, "fallback") || strings.Contains(n, "代替") || strings.Contains(n, "フォールバック") || strings.Contains(n, "バックアップ") {
		return roleFallback
	}
	return rolePrimary
}

var (
	llmWord    = regexp.MustCompile(`(?i)(llm|言語モデル|代替|フォールバック|fallback|プライマリ|primary|モデル)`)
	changeWord = regexp.MustCompile(`(?i)(割り当て|割当|にして|にする|にしたい|に変|へ変|変えて|変更|切り替|切替|設定|使って|使いたい|set|assign|switch)`)
	inquiry    = regexp.MustCompile(`(?i)(教えて|見せて|一覧|確認して|とは|何です|なんです|[かが][？?]|\?|？)\s*$`)
	mediaWord  = regexp.MustCompile(`(?i)(画像|動画|イメージ|image|video|quality|クオリティ|品質|参照|リファレンス|keyframe|プレビュー)`)
	// the model the message names: "…を Claude Opus にして"
	namedModel = regexp.MustCompile(`(?:を|は)\s*(.+?)\s*(?:に|へ)\s*(?:して|する|したい|変|切り替|切替|設定)`)
)

// person is the "モデル" of a fashion model or of the cast ("プロのモデル", "モデルさん"): no LLM.
var person = regexp.MustCompile(`(?:プロ|出演者?|ファッション|専属|人物|女性|男性|外国人|日本人|ヘア|ハンド)の?モデル|モデル(?:さん|ちゃん|君|くん|たち|達)`)

// Asked reports whether the message asks to change the LLM of an Agent: it names an Agent (by its name or label), says LLM, a model or the role of its route, and does something to
// it. A question ("Director の LLM は何？"), a message about the image or video models and a person who is a model are not.
func Asked(message string) bool {
	message = person.ReplaceAllString(message, "")
	if inquiry.MatchString(strings.TrimSpace(message)) || mediaWord.MatchString(message) {
		return false
	}
	return llmWord.MatchString(message) && changeWord.MatchString(message) && mentionsAgent(message)
}

// detect reads "Director の LLM を Claude Opus にして" without the model, so that the LLM itself can be assigned while none is set up.
func detect(message string) []commandinterpreter.Operation {
	if !Asked(message) {
		return nil
	}
	m := namedModel.FindStringSubmatch(message)
	if m == nil {
		return nil
	}
	a := Args{Agent: agentIn(message), Role: roleOf(message), Model: m[1]}
	if id, ok := providerOf(m[1]); ok {
		a.Provider = id
	}
	op, err := commandinterpreter.NewOperation(OpSetLLMAssignment, a)
	if err != nil {
		return nil
	}
	return []commandinterpreter.Operation{op}
}
