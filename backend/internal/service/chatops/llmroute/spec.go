// Package llmroute is the chat operation of the LLM assignment (#1140; spec/components/chat-settings-operations.md §3): the director asks for "Director の LLM を Claude Opus にして",
// "Producer の代替を GPT にして" and gets a card that shows the route of the Agent before and after; the answer saves it through the same llmassign.Service.Save as the settings
// screen and the MCP tool set_llm_assignment: only the named Agent and role change, the other half of its route stays. A model that is in no list of its provider is allowed, with a
// warning in the card, as in the MCP tool. The assignment is a setting of the whole app, which the card says.
package llmroute

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
)

// OpSetLLMAssignment changes the provider and model of one role of one Agent's route.
const OpSetLLMAssignment commandinterpreter.OpType = "set_llm_assignment"

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
			providerList() + ` (leave it out when the message does not say). "args.model" is the model name as the director said it; ` +
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

// providerList is the providers the model may name, as the program has them (llm.Providers), written for the Doc.
func providerList() string {
	var quoted []string
	for _, p := range llm.Providers() {
		quoted = append(quoted, `"`+string(p)+`"`)
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

// clean keeps the operation the model returned with its arguments in their normal form (the role as primary or fallback, the provider as an ID when the text names one).
func clean(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return commandinterpreter.Operation{}, false
	}
	a.Agent, a.Model = strings.TrimSpace(a.Agent), strings.TrimSpace(a.Model)
	a.Role = nlu.RoleOf(a.Role)
	if id, ok := providerOf(a.Provider); ok {
		a.Provider = id
	} else {
		a.Provider = ""
	}
	out, err := commandinterpreter.NewOperation(OpSetLLMAssignment, a)
	return out, err == nil
}

var (
	llmWord    = regexp.MustCompile(`(?i)(llm|言語モデル|代替|フォールバック|fallback|プライマリ|primary|モデル)`)
	changeWord = regexp.MustCompile(`(?i)(割り当て|割当|にして|にする|にしたい|に変|へ変|変えて|変更|切り替|切替|設定|使って|使いたい|set|assign|switch)`)
	mediaWord  = regexp.MustCompile(`(?i)(画像|動画|イメージ|image|video|quality|クオリティ|品質|参照|リファレンス|keyframe|プレビュー)`)
	// the model the message names: "…を Claude Opus にして". The greedy prefix takes the name after the last を/は ("Director は LLM を Claude Opus にして" names Claude Opus); a comma or
	// space between the particle and the name is not part of it ("Director の LLM を、Claude Opus にして").
	namedModel = regexp.MustCompile(`.*(?:を|は)[\s、,，]*(.+?)\s*(?:に|へ)\s*(?:して|する|したい|変|切り替|切替|設定)`)
)

// sentences splits a message where a clause ends (the same ends as outofscope: nlu.ClauseEnd).
var sentences = nlu.ClauseEnd

// Asked reports whether the message asks to change the LLM of an Agent: it names an Agent (by its name or label), says LLM, a model or the role of its route, and does something to
// it. A question ("Director の LLM は何？"), a message about the image or video models and a person who is a model are not.
func Asked(message string) bool {
	message = nlu.Person.ReplaceAllString(message, "")
	if nlu.IsInquiry(message) || mediaWord.MatchString(message) {
		return false
	}
	return llmWord.MatchString(message) && changeWord.MatchString(message) && nlu.AgentIn(message) != ""
}

// namesRouteModel reports whether one sentence puts a model on a route without saying LLM: it names an Agent or a role (代替, バックアップ ...) and a name that can be a model's
// ("Producer は GPT にして"). A word that is no model name ("Producer は据え置きにして", "モデルはそのままにして") is no second route.
func namesRouteModel(sentence string) bool {
	m := namedModel.FindStringSubmatch(sentence)
	return m != nil && !nlu.IsInquiry(sentence) && modelLike(m[1]) && (nlu.AgentIn(sentence) != "" || llmWord.MatchString(sentence))
}

// modelLike reports whether a name can be a model's: it names a provider's model ("Claude Opus"), or has an ASCII letter or digit ("o3 mini", "Qwen 3 の 32B", "速い Gemma") or a katakana ("ミストラル").
// It is none when it is clearly something else: a word that keeps the route ("キープ", "据え置き"), says "the same" or a limit ("Director と同じ", "デフォルトのまま", "3 回まで", "JSON で返すように"), or names an
// Agent ("Director の設定", "Director より速い"); and a count, a duration or a degree ("3 回", "5 秒", "丁寧な English", "一緒") is none unless a model token stands in it (hasModelToken: "R1 の 高速版",
// "速い Gemma", "ミストラル 長文版"). A word of hiragana and kanji only ("慎重な性格") is none.
func modelLike(name string) bool {
	if _, named := providerOf(name); named {
		return true
	}
	if keepWord.MatchString(name) || notModel.MatchString(name) || len(nlu.AgentsIn(name)) > 0 {
		return false
	}
	if degreeWord.MatchString(name) && !hasModelToken(name) {
		return false
	}
	return strings.IndexFunc(name, func(r rune) bool {
		return (r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))) || unicode.Is(unicode.Katakana, r)
	}) >= 0
}

// hasModelToken reports whether a name holds a word of a model's name: an ASCII word with a letter and a digit ("R1", "o3", "32B") or with 3 or more letters ("Gemma"), or a katakana run of 2 or more
// characters ("ミストラル", "ラマ", "ﾗｰ"; the half-width 「ｰ」「ﾞ」「ﾟ」 are part of a run like the full-width 「ー」), once the names of languages and formats (genericWord) are taken out.
func hasModelToken(name string) bool {
	for _, w := range modelToken.FindAllString(genericWord.ReplaceAllString(name, " "), -1) {
		if strings.IndexFunc(w, unicode.IsLetter) >= 0 && (len(w) >= 3 || strings.IndexFunc(w, unicode.IsDigit) >= 0) {
			return true
		}
	}
	return false
}

var (
	roleWord = regexp.MustCompile(`(?i)(fallback|代替|フォールバック|バックアップ)`)
	// topicRole is a clause that ends on a role as a topic ("Producer の代替は", "Director のバックアップって", "代替なら"): the clause after it says what is done to it.
	topicRole = regexp.MustCompile(`(?i)(?:fallback|代替|フォールバック|バックアップ)\s*(?:は|って|なら|について|の件)\s*$`)
	// keepWord says a route is left as it is ("代替はそのままで", "残して", "触らずに", "変えずに"): such a clause names a role without asking to change it.
	keepWord = regexp.MustCompile(`(?i)残|そのまま|のまま|触|変え(?:ず|ない)|維持|据え置き|キープ|keep`)
	// notModel is what a word that sets no model says: "Director と同じ", "デフォルト", "3 回まで", "返すように".
	notModel = regexp.MustCompile(`同じ|デフォルト|まで|ように|返す`)
	// degreeWord is a count, a duration or a degree ("3 回", "5 秒", "一緒", "より速い"); genericWord is a name of a language or a format, no model's; modelToken finds the words that may be a model's
	// (an ASCII run of letters and digits, or a katakana run of 2 or more characters: hasModelToken tells which are).
	degreeWord  = regexp.MustCompile(`回|秒|分|個|つ|一緒|同様|設定|より|丁寧|速|遅|短|長`)
	genericWord = regexp.MustCompile(`(?i)\b(?:English|Japanese|JSON|XML|YAML|Markdown)\b`)
	modelToken  = regexp.MustCompile(`[A-Za-z0-9]+|[\p{Katakana}ーｰﾞﾟ]{2,}`)
)

// roleOf is the role the sentence that asks says; a sentence with no role word takes the role from the clause just before it when that is a topic of the role ("Producer の代替は、LLM を Gemini に
// して" is the fallback) that keeps nothing ("Director の代替は残して、LLM を Claude Opus にして" is the primary). No other clause is read.
func roleOf(sentence, before string) string {
	if roleWord.MatchString(sentence) || !topicRole.MatchString(before) || keepWord.MatchString(before) {
		return nlu.RoleOf(sentence)
	}
	return nlu.RoleOf(before)
}

// keepsAgent reports whether a clause leaves a whole Agent as it is ("Producer は据え置きで", "Producer は触らずに"): it has a word that keeps and no role word. A clause that keeps one role
// ("Director の代替は残して") names the Agent whose other role is changed, so it is not one.
func keepsAgent(clause string) bool {
	return keepWord.MatchString(clause) && !roleWord.MatchString(clause)
}

// agentsOfRequest lists the Agents the clauses of a message name, but those a clause leaves as they are (keepsAgent): "Producer は据え置きで、LLM を GPT にして" names no Agent to change, so the
// chat asks which, and never takes the Agent that is kept.
func agentsOfRequest(clauses []string) []string {
	var asked []string
	for _, c := range clauses {
		if !keepsAgent(c) {
			asked = append(asked, c)
		}
	}
	return nlu.AgentsIn(strings.Join(asked, "、"))
}

// detect reads "Director の LLM を Claude Opus にして" without the model, so that the LLM itself can be assigned while none is set up.
func detect(message string) []commandinterpreter.Operation {
	if !Asked(message) {
		return nil
	}
	// One sentence is read for its own Agent, role and model. A message that changes two routes is left to the model (the values of one sentence must not be mixed with another's), also when
	// the second sentence only names an Agent or a role and the model ("Director の LLM を Claude Opus にして、Producer は GPT にして").
	clauses := sentences.Split(message, -1)
	var asked []int
	for i, sentence := range clauses {
		if Asked(sentence) || namesRouteModel(sentence) {
			asked = append(asked, i)
		}
	}
	if len(asked) > 1 {
		return nil
	}
	sentence, before := message, ""
	if len(asked) == 1 {
		sentence = clauses[asked[0]]
		if asked[0] > 0 {
			before = clauses[asked[0]-1]
		}
	}
	m := namedModel.FindStringSubmatch(sentence)
	if m == nil {
		return nil
	}
	a := Args{Agent: nlu.AgentIn(sentence), Role: roleOf(sentence, before), Model: m[1]}
	if a.Agent == "" { // the Agent stands in another clause ("Director は、LLM を Claude Opus にして"): the one the clause before names, else the one the message names (two: the chat asks which)
		near := sentence
		if !keepWord.MatchString(before) { // a clause that keeps a route names the Agent it keeps ("Producer は据え置きで"), not the one asked
			near = before + "、" + sentence
		}
		if agents := nlu.AgentsIn(near); len(agents) == 1 {
			a.Agent = agents[0]
		} else if agents = agentsOfRequest(clauses); len(agents) == 1 {
			a.Agent = agents[0]
		}
	}
	if id, ok := providerOf(m[1]); ok {
		a.Provider = id
	}
	op, err := commandinterpreter.NewOperation(OpSetLLMAssignment, a)
	if err != nil {
		return nil
	}
	return []commandinterpreter.Operation{op}
}
