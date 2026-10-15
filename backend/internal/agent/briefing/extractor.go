// Package briefing extracts a Creative Brief from what the user writes in the chat
// (F-01, spec/spec.md §4.2).
//
// RuleExtractor is deterministic and needs no external API, so the chat works before
// a Reasoning LLM Adapter is configured. An LLM-backed Extractor can replace it
// without changing the callers.
package briefing

import (
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
	platformRe   = regexp.MustCompile(`\b(Instagram|YouTube|TikTok|Facebook|LINE|Twitter)\s*(広告|投稿|リール|ショート|CM)?`)
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

var objectiveKeywords = []string{"ブランドサイト", "Webサイト", "店頭", "展示会", "採用", "ECサイト"}

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
	b.Objective = objective(text)
	b.Subject = subject(text)
	b.Target = target(text)
	b.Duration = duration(text)
	b.AspectRatio = aspectRatio(text)
	b.Mood = mood(text)
	for _, u := range urlRe.FindAllString(text, -1) {
		// A URL the Brief form would refuse stays out, or it would block saving the form later.
		if u = strings.TrimRight(u, ".,;"); ReferenceProblem(u) == "" {
			b.References = append(b.References, u)
		}
	}
	return b
}

// subject is the noun in front of "の縦型 / の15秒 / のCM ..."; a content-type word
// there ("アニメの動画") names the kind of video, not the subject.
func subject(text string) string {
	m := subjectRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	for _, r := range contentTypeRules {
		for _, k := range r.keywords {
			if strings.Contains(m[1], k) {
				return ""
			}
		}
	}
	return m[1]
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
