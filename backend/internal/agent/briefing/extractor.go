// Package briefing extracts a Creative Brief from what the user writes in the chat
// (F-01, spec/spec.md §4.2).
//
// RuleExtractor is deterministic and needs no external API, so the chat works before
// a Reasoning LLM Adapter is configured. An LLM-backed Extractor can replace it
// without changing the callers.
package briefing

import (
	"cmp"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Extractor turns free text into the Brief items it mentions. Items not found stay empty.
type Extractor interface {
	Extract(text string) domain.Brief
}

// RuleExtractor is the keyword / pattern based Extractor.
type RuleExtractor struct{}

var (
	// durationRe reads "15秒", "1.5秒", "2分" and the compound "1分30秒" as one value.
	durationRe   = regexp.MustCompile(`\d{1,3}(?:\.\d+)?\s*分(?:\s*\d{1,3}(?:\.\d+)?\s*秒)?|\d{1,3}(?:\.\d+)?\s*秒`)
	ratioRe      = regexp.MustCompile(`(\d{1,2})\s*[:：]\s*(\d{1,2})`)
	targetRe     = regexp.MustCompile(`(\d{1,2})(?:\s*[〜~～\-ー]\s*(\d{1,2}))?代(男性|女性|男女|層)?`)
	subjectRe    = regexp.MustCompile(`(?:^|[\s、。,を])([^\s、。,！!？?「」のを]{1,30})の(?:縦型|横型|縦長|横長|正方形|\d+\s*秒|CM|動画|広告|PV|ショート|アニメ|ドラマ|ムービー)`)
	whitespaceRe = regexp.MustCompile(`\s+`)
	urlRe        = regexp.MustCompile(`https?://[^\s、。」）)]+`)
	platformRe   = regexp.MustCompile(`\b(Instagram|YouTube|TikTok|Facebook|LINE|Twitter|Reels|Shorts)\s*(広告|投稿|リール|ショート|CM)?`)
	// labelledRe reads "ターゲット: 主婦", "雰囲気は ゆるい": a value the user names by its item, whatever the value is.
	labelledRe = regexp.MustCompile(`(?:^|[\s、。,])(ターゲット|対象|Target|雰囲気|トーン|Mood|目的|用途|Objective)(?:は|：|:)\s*([^\s。、,]{1,60})`)
	themeRe    = regexp.MustCompile(`テーマ(?:は|：|:)\s*([^。\n]{1,80})`)
)

type keywordRule struct {
	value    string
	keywords []string
}

// contentTypeRules are checked in order; the first rule with a hit wins.
var contentTypeRules = []keywordRule{
	{"ドラマ", []string{"ドラマ"}},
	{"アニメ", []string{"アニメ"}},
	{"商品CM", []string{"CM", "コマーシャル", "商品紹介", "広告"}},
	{"SNS/ショート動画", []string{"SNS", "ショート動画", "リール", "Reels", "Shorts", "TikTok"}},
}

var objectiveKeywords = []string{"ブランドサイト", "Webサイト", "店頭", "展示会", "採用", "ECサイト", "認知拡大", "集客", "宣伝", "ファン獲得", "フォロワー獲得"}

// constraintRules are the constraints the extractor keeps in the Additional Instruction under their canonical label, so that the
// Producer, Writer and Director are told them as fixed conditions (#1114). A constraint is a safety condition: a keyword counts only
// when constraintHit finds it stated, never when it may be negated, so an uncertain one is not recorded.
var constraintRules = []keywordRule{
	{"成人", []string{"成人", "18歳以上", "20歳以上"}},
	{"架空", []string{"架空", "フィクション", "実在しない", "実在の人物ではない"}},
	{"着衣", []string{"着衣", "服を着", "服は着", "服装あり", "露出なし", "露出しない"}},
}

var (
	// notStatedAfterRe: what follows the keyword makes it another word ("成人式", "成人向け", "服を着替える").
	notStatedAfterRe = regexp.MustCompile(`^(?:向け|式|替)`)
	// negationRe is a negation in the rest of the clause ("成人ではない", "服は着ていない設定", "着衣なし").
	negationRe = regexp.MustCompile(`ない|ません|なし|なく`)
	// notStatedBeforeRe: the keyword is the end of a word that negates it ("ノンフィクション", "非実在").
	notStatedBeforeRe = regexp.MustCompile(`(?:ノン|非|未)$`)
	clauseEndRe       = regexp.MustCompile(`[、,，。\n]`)
)

// constraintHit reports whether the text states the constraint of one of the keywords: some occurrence is neither the end of a word that
// negates it nor followed, in its clause, by a negation.
func constraintHit(text string, keywords []string) bool {
	for _, k := range keywords {
		for from := 0; from < len(text); {
			i := strings.Index(text[from:], k)
			if i < 0 {
				break
			}
			start := from + i
			from = start + len(k)
			rest := text[from:]
			if loc := clauseEndRe.FindStringIndex(rest); loc != nil {
				rest = rest[:loc[0]]
			}
			if !notStatedBeforeRe.MatchString(text[:start]) && !notStatedAfterRe.MatchString(rest) && !negationRe.MatchString(rest) {
				return true
			}
		}
	}
	return false
}

var targetKeywords = []string{"ファミリー層", "ファミリー", "学生", "主婦", "ビジネスパーソン", "経営者"}

var moodKeywords = []string{
	"高級", "上品", "ミニマル", "夜", "ポップ", "明るい", "クール", "スタイリッシュ", "ナチュラル", "温かい",
	"可愛い", "かわいい", "ダーク", "落ち着いた", "エモい", "爽やか", "楽しい", "感動", "未来的", "レトロ", "シネマティック",
}

var ratioKeywords = []struct {
	ratio    string
	keywords []string
}{
	{domain.AspectRatio9x16, []string{"縦型", "縦長", "縦動画", "縦向き"}},
	{domain.AspectRatio16x9, []string{"横型", "横長", "横動画", "横向き"}},
	{domain.AspectRatio1x1, []string{"正方形", "スクエア"}},
}

// Extract implements Extractor.
func (RuleExtractor) Extract(text string) domain.Brief {
	var b domain.Brief
	b.ContentType = firstRule(text, contentTypeRules)
	b.Objective = cmp.Or(labelled(text, domain.BriefObjective), objective(text))
	b.Subject = statedSubject(text)
	// The age and gender of the protagonist ("30代女性インフルエンサーを主人公に") are not the audience.
	b.Target = cmp.Or(labelled(text, domain.BriefTarget), target(strings.Replace(text, b.Subject, "", 1)))
	b.Duration = duration(text)
	b.AspectRatio = aspectRatio(text)
	b.Mood = cmp.Or(labelled(text, domain.BriefMood), mood(text))
	b.AdditionalInstruction = instruction(text)
	for _, u := range urlRe.FindAllString(text, -1) {
		// A URL the Brief form would refuse stays out, or it would block saving the form later.
		if u = strings.TrimRight(u, ".,;"); ReferenceProblem(u) == "" {
			b.References = append(b.References, u)
		}
	}
	return b
}

// labelledFields maps the labels labelledRe reads to the Brief item they name.
var labelledFields = map[string]domain.BriefField{
	"ターゲット": domain.BriefTarget, "対象": domain.BriefTarget, "Target": domain.BriefTarget,
	"雰囲気": domain.BriefMood, "トーン": domain.BriefMood, "Mood": domain.BriefMood,
	"目的": domain.BriefObjective, "用途": domain.BriefObjective, "Objective": domain.BriefObjective,
}

// labelled is the value the text gives to the item f by name ("ターゲット: 主婦"), or "".
func labelled(text string, f domain.BriefField) string {
	for _, m := range labelledRe.FindAllStringSubmatch(text, -1) {
		if labelledFields[m[1]] == f {
			return trimTail(m[2])
		}
	}
	return ""
}

// instruction is the theme and the constraints the text states, one per line: "テーマ: …", "制約: 成人・架空".
func instruction(text string) string {
	var lines []string
	if m := themeRe.FindStringSubmatch(text); m != nil {
		if theme := strings.TrimSpace(m[1]); concrete(theme) != "" {
			lines = append(lines, ThemePrefix+theme)
		}
	}
	var labels []string
	for _, r := range constraintRules {
		if constraintHit(text, r.keywords) {
			labels = append(labels, r.value)
		}
	}
	if len(labels) > 0 {
		lines = append(lines, ConstraintPrefix+strings.Join(labels, "・"))
	}
	return strings.Join(lines, "\n")
}

func firstRule(text string, rules []keywordRule) string {
	for _, r := range rules {
		for _, k := range r.keywords {
			if strings.Contains(text, k) {
				return r.value
			}
		}
	}
	return ""
}

func objective(text string) string {
	if m := platformRe.FindStringSubmatch(text); m != nil {
		return m[1] + m[2]
	}
	for _, k := range objectiveKeywords {
		if strings.Contains(text, k) {
			return k
		}
	}
	return ""
}

func target(text string) string {
	if m := targetRe.FindStringSubmatch(text); m != nil {
		if m[2] != "" {
			return fmt.Sprintf("%s〜%s代%s", m[1], m[2], m[3])
		}
		return m[1] + "代" + m[3]
	}
	for _, k := range targetKeywords {
		if strings.Contains(text, k) {
			return k
		}
	}
	return ""
}

func duration(text string) string {
	return whitespaceRe.ReplaceAllString(durationRe.FindString(text), "")
}

func aspectRatio(text string) string {
	// Only ratios generation supports are taken; a clock time ("10:30") or another ratio ("4:3")
	// falls through to the keywords instead of silently becoming a 16:9 generation.
	for _, m := range ratioRe.FindAllStringSubmatch(text, -1) {
		if ratio := m[1] + ":" + m[2]; domain.IsBriefAspectRatio(ratio) {
			return ratio
		}
	}
	for _, r := range ratioKeywords {
		for _, k := range r.keywords {
			if strings.Contains(text, k) {
				return r.ratio
			}
		}
	}
	return ""
}

// mood joins the mood words in the order they appear in the text.
func mood(text string) string {
	type hit struct {
		pos  int
		word string
	}
	var hits []hit
	for _, k := range moodKeywords {
		if i := strings.Index(text, k); i >= 0 {
			hits = append(hits, hit{i, k})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	words := make([]string, len(hits))
	for i, h := range hits {
		words[i] = h.word
	}
	return strings.Join(words, "・")
}
