// Package nlu holds the small readings of a director's sentence that the chat operations share: how a name is compared, which words make a "モデル" a person, which sentence is a
// question, which role a text means and which Agent a message names. The operations (install, llmroute, assign ...) decide for themselves what a sentence asks for; this is only
// the vocabulary they have in common, in one place so that two copies cannot drift apart.
package nlu

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
)

// The two roles of a route.
const (
	RolePrimary  = "primary"
	RoleFallback = "fallback"
)

// Norm is text as it is compared: lower case, with only letters and digits ("claude-opus-4" and "Claude Opus 4" meet; "FLUX.1 schnell" and "flux1-schnell" are "flux1schnell").
func Norm(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Few lists the first n names, and how many more there are.
func Few(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, "、")
	}
	return strings.Join(names[:n], "、") + "、ほか " + strconv.Itoa(len(names)-n) + " 件"
}

// ClauseEnd is where a clause of a message ends (a comma, a stop, a ! or ? of either width, a semicolon, a line break, or an ASCII period before a space). The operations read the
// words of one request from one clause, so that the values of one clause are not mixed with another's ("Director の LLM を Claude にして、Producer の LLM を GPT にして").
var ClauseEnd = regexp.MustCompile(`[、。，．！？!?;；\n]|[,.]\s`)

// Person is the "モデル" of a fashion model or of the cast ("プロのモデル", "モデルさん"): it names no model of the program, so a sentence is read without it.
var Person = regexp.MustCompile(`(?:プロ|出演者?|ファッション|専属|人物|女性|男性|外国人|日本人|ヘア|ハンド)の?モデル|モデル(?:さん|ちゃん|君|くん|たち|達)`)

var (
	inquiry = regexp.MustCompile(`(?i)(教えて|見せて|一覧|確認して|とは|何です|なんです|[かが][？?]|\?|？)\s*$`)
	// "…にしてくれる？", "…にしてもらえますか？": a request in the form of a question, not a question about the setting.
	requestForm = regexp.MustCompile(`(?i)(?:にし|に変え|へ変え|変更し|切り替え|切替え|設定し|使っ)(?:て|で)(?:くれ|もらえ|もらいたい|ください|下さい|いただ|頂)`)
)

// IsInquiry reports whether the message is a question ("今の画像モデルは何？", "…を教えて"): it ends in a question or an ask to be told. A request in the form of a question
// ("Director の LLM を Claude にしてくれる？") is not one.
func IsInquiry(message string) bool {
	return inquiry.MatchString(strings.TrimSpace(message)) && !requestForm.MatchString(message)
}

// RoleOf is primary unless the text says the alternative ("fallback", "代替", "フォールバック", "バックアップ").
func RoleOf(text string) string {
	n := Norm(text)
	if strings.Contains(n, "fallback") || strings.Contains(n, "代替") || strings.Contains(n, "フォールバック") || strings.Contains(n, "バックアップ") {
		return RoleFallback
	}
	return RolePrimary
}

// AgentKeys are the names an Agent is called by: its ID ("art_director"), its label ("Art Director（Reference Pack）") and the label without its note ("Art Director"),
// normalised.
func AgentKeys(id, label string) []string {
	keys := []string{Norm(id), Norm(label)}
	if i := strings.IndexAny(label, "（("); i > 0 {
		keys = append(keys, Norm(label[:i]))
	}
	return keys
}

// AgentIn finds the Agent of the program a message names: the longest of its names the message holds ("Art Director" is not "Director"); "" for none.
func AgentIn(message string) string {
	n := Norm(message)
	best, bestLen := "", 0
	for _, a := range llm.Agents() {
		for _, k := range AgentKeys(string(a), failmsg.Label(a)) {
			if len(k) > bestLen && strings.Contains(n, k) {
				best, bestLen = string(a), len(k)
			}
		}
	}
	return best
}
