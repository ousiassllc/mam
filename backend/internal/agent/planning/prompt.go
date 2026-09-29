// Package planning holds the Agents of the planning stages of the AUTO pipeline (spec/spec.md
// §4.3): Producer (Concept / Structure), Writer (Script), Director (Scene / Shot Planning) and
// Storyboard (a frame per Shot). Each Agent turns structured state into a prompt, reasons
// through the Reasoning LLM Adapter (llm.Completer) and validates the structured answer, so the
// Orchestrator never stores output that is unusable downstream.
package planning

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// systemPrefix is shared by every planning prompt.
const systemPrefix = "あなたは AI 動画制作チームの一員です。" +
	"与えられた Creative Brief に忠実に、指示された JSON オブジェクトだけを返してください。" +
	"説明文・前置き・コードフェンスは付けないでください。" +
	"値の文章は日本語で書き、Brief に無い固有名詞や事実（商品の効能・価格・受賞歴など）を作らないでください。\n\n"

// templateGuide is the emphasis of each Production Template (spec/spec.md §19).
func templateGuide(t domain.ProductionTemplate) string {
	switch t {
	case domain.TemplateDrama:
		return "ドラマ: 登場人物の感情の変化と場面のつながりを重視する。"
	case domain.TemplateAnime:
		return "アニメ: キャラクターの一貫した造形と、動きが分かりやすい演出を重視する。"
	case domain.TemplateSNSShort:
		return "SNS/ショート動画: 冒頭 1〜2 秒のフック、テンポの速さ、最後の一言（CTA）を重視する。"
	}
	return "商品CM: 冒頭のフック、商品の見せ場（Product Exposure）、最後の CTA を重視する。"
}

// briefText renders the Creative Brief for a prompt. "AIに任せる" fields are stated as such.
func briefText(b domain.Brief, t domain.ProductionTemplate) string {
	var sb strings.Builder
	for _, f := range domain.RequiredBriefFields() {
		v := b.Get(f)
		switch {
		case v != "":
		case b.IsDelegated(f):
			v = "（AI に任せる: 最も妥当なものを選ぶ）"
		default:
			v = "（未指定）"
		}
		fmt.Fprintf(&sb, "- %s: %s\n", f.Label(), v)
	}
	if b.AdditionalInstruction != "" {
		fmt.Fprintf(&sb, "- 追加指示: %s\n", b.AdditionalInstruction)
	}
	if n := len(b.Assets); n > 0 {
		fmt.Fprintf(&sb, "- 添付画像: %d 点（商品・人物などの素材として使う）\n", n)
	}
	if n := len(b.References); n > 0 {
		fmt.Fprintf(&sb, "- 参考資料: %d 点\n", n)
	}
	fmt.Fprintf(&sb, "- 制作テンプレート: %s\n", templateGuide(t))
	return sb.String()
}

// feedbackText is the user's change request and the version it applies to, or "".
func feedbackText(previous any, feedback string) string {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return ""
	}
	return "\n## 前回の案\n" + mustJSON(previous) + "\n\n## ユーザーからの修正依頼（必ず反映する）\n" + feedback + "\n"
}

func mustJSON(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil { // the inputs are plain structs of strings and numbers
		panic(err)
	}
	return string(raw)
}

var durationRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(秒|sec|s|分|min|m)`)

// ParseSeconds reads a Brief Duration such as "15秒", "30 sec", "1分" or "1分30秒" into seconds.
func ParseSeconds(s string) (float64, bool) {
	total := 0.0
	for _, m := range durationRe.FindAllStringSubmatch(strings.ToLower(strings.TrimSpace(s)), -1) {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		if m[2] == "分" || m[2] == "min" || m[2] == "m" {
			n *= 60
		}
		total += n
	}
	return total, total > 0
}

// BriefText renders the Creative Brief for the prompt of an Agent outside this package.
func BriefText(b domain.Brief, t domain.ProductionTemplate) string { return briefText(b, t) }

// Invalidf reports an answer that is valid JSON but unusable (it wraps ErrInvalidPlan), for
// Agents outside this package that validate their own answers.
func Invalidf(format string, args ...any) error { return invalidf(format, args...) }
